package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Job struct {
	Path     string `json:"path"`
	Filename string `json:"filename"`
	Kind     string `json:"kind"` // "xml" ou "zip"
}

type RabbitMQ struct {
	conn       *amqp.Connection
	ch         *amqp.Channel
	queueName  string
	confirmCh  <-chan amqp.Confirmation
	maxRetries int
	prefetch   int
}

func NewRabbitMQ(url, queueName string) (*RabbitMQ, error) {
	// defaults
	maxRetries := 3
	prefetch := 10

	if v := os.Getenv("NFE_DROP_RABBITMQ_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxRetries = n
		}
	}

	if v := os.Getenv("NFE_DROP_RABBITMQ_PREFETCH"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			prefetch = n
		}
	}

	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("erro conectando no RabbitMQ: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro abrindo canal no RabbitMQ: %w", err)
	}

	// DLX + DLQ
	dlxName := queueName + ".dlx"
	dlqName := queueName + ".dlq"

	if err := ch.ExchangeDeclare(
		dlxName,
		"direct",
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("erro declarando exchange DLX %q: %w", dlxName, err)
	}

	if _, err := ch.QueueDeclare(
		dlqName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("erro declarando fila DLQ %q: %w", dlqName, err)
	}

	if err := ch.QueueBind(
		dlqName,
		dlqName,
		dlxName,
		false,
		nil,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("erro bindando DLQ %q no DLX %q: %w", dlqName, dlxName, err)
	}

	// fila principal com DLX configurado
	args := amqp.Table{
		"x-dead-letter-exchange":    dlxName,
		"x-dead-letter-routing-key": dlqName,
	}

	if _, err := ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		args,
	); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("erro declarando fila %q: %w", queueName, err)
	}

	// QoS (prefetch)
	if err := ch.Qos(prefetch, 0, false); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("erro configurando QoS (prefetch=%d): %w", prefetch, err)
	}

	// publisher confirms
	if err := ch.Confirm(false); err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("erro habilitando publisher confirms: %w", err)
	}

	confirmCh := ch.NotifyPublish(make(chan amqp.Confirmation, prefetch*2))

	return &RabbitMQ{
		conn:       conn,
		ch:         ch,
		queueName:  queueName,
		confirmCh:  confirmCh,
		maxRetries: maxRetries,
		prefetch:   prefetch,
	}, nil
}

func (r *RabbitMQ) publish(ctx context.Context, body []byte, headers amqp.Table) error {
	if headers == nil {
		headers = amqp.Table{}
	}
	if _, ok := headers["x-retries"]; !ok {
		headers["x-retries"] = int32(0)
	}

	err := r.ch.PublishWithContext(
		ctx,
		"", // exchange padrão
		r.queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         body,
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			Headers:      headers,
		},
	)
	if err != nil {
		return fmt.Errorf("erro publicando mensagem no RabbitMQ: %w", err)
	}

	// Espera confirmação do broker
	select {
	case conf := <-r.confirmCh:
		if !conf.Ack {
			return fmt.Errorf("mensagem não confirmada pelo broker")
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (r *RabbitMQ) PublishJob(ctx context.Context, job Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("erro serializando job: %w", err)
	}

	return r.publish(ctx, body, amqp.Table{
		"x-retries": int32(0),
	})
}

func (r *RabbitMQ) ConsumeJobs(ctx context.Context, handler func(Job) error) error {
	msgs, err := r.ch.Consume(
		r.queueName,
		"",
		false, // autoAck
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return fmt.Errorf("erro iniciando consumo do RabbitMQ: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case msg, ok := <-msgs:
			if !ok {
				return fmt.Errorf("canal de mensagens encerrado")
			}

			var job Job
			if err := json.Unmarshal(msg.Body, &job); err != nil {
				slog.Error("erro de unmarshal de job do RabbitMQ", "err", err)
				_ = msg.Ack(false)
				continue
			}

			if err := handler(job); err != nil {
				// erro do handler → retry ou DLQ
				retries := extractRetries(msg.Headers)

				if retries < r.maxRetries {
					slog.Warn("erro processando job, reenfileirando",
						"path", job.Path,
						"filename", job.Filename,
						"kind", job.Kind,
						"retries", retries,
						"max_retries", r.maxRetries,
						"err", err,
					)

					headers := msg.Headers
					if headers == nil {
						headers = amqp.Table{}
					}
					headers["x-retries"] = int32(retries + 1)

					pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					if perr := r.publish(pubCtx, msg.Body, headers); perr != nil {
						slog.Error("falha ao reenfileirar job", "err", perr)
					}
					cancel()

					_ = msg.Ack(false)
				} else {
					slog.Error("erro processando job, enviando para DLQ",
						"path", job.Path,
						"filename", job.Filename,
						"kind", job.Kind,
						"retries", retries,
						"max_retries", r.maxRetries,
						"err", err,
					)
					// Nack sem requeue → vai pro DLQ por causa do DLX
					_ = msg.Nack(false, false)
				}

				continue
			}

			_ = msg.Ack(false)
		}
	}
}

func (r *RabbitMQ) Close() error {
	if r.ch != nil {
		_ = r.ch.Close()
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func extractRetries(h amqp.Table) int {
	if h == nil {
		return 0
	}
	v, ok := h["x-retries"]
	if !ok {
		return 0
	}

	switch t := v.(type) {
	case int32:
		return int(t)
	case int64:
		return int(t)
	case float32:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}
