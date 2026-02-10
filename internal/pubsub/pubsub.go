package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

type SimpleQueueType int

const (
	DurableQueue SimpleQueueType = iota
	TransientQueue
)

func DeclareAndBind(conn *amqp.Connection, exchange, queueName, key string, queueType SimpleQueueType) (
	*amqp.Channel, amqp.Queue, error) {
	channel, err := conn.Channel()
	if err != nil {
		fmt.Printf("Got an error creating a channel: %v\n", err)
		return nil, amqp.Queue{}, err
	}

	isDurable := queueType == DurableQueue
	autoDelete := queueType == TransientQueue
	exclusive := queueType == TransientQueue

	queue, err := channel.QueueDeclare(queueName, isDurable, autoDelete, exclusive, false, nil)
	if err != nil {
		fmt.Printf("Got an error creating a queue: %v\n", err)
		return nil, amqp.Queue{}, err
	}

	err = channel.QueueBind(queueName, key, exchange, false, nil)
	if err != nil {
		fmt.Printf("Got an error binding to a queue: %v\n", err)
		return nil, amqp.Queue{}, err
	}

	return channel, queue, nil
}

func PublishJSON[T any](ch *amqp.Channel, exchange, key string, val T) error {
	jsonBytes, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("Got an error Marshalling val into json bytes")
	}

	ch.PublishWithContext(context.Background(), exchange, key, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        jsonBytes,
	})
	return nil
}

func SubscribeJSON[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType,
	wg *sync.WaitGroup,
	handler func(T),
) error {
	channel, _, err := DeclareAndBind(conn, exchange, queueName, key, queueType)
	if err != nil {
		fmt.Printf("Got an error creating and binding a queue: %v", err)
		return err
	}

	deliveryChannel, err := channel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("failed to consume: %w", err)
	}

	wg.Add(1)

	go func() {
		defer wg.Done()
		defer channel.Close()

		for d := range deliveryChannel {
			var payload T
			if err := json.Unmarshal(d.Body, &payload); err != nil {
				fmt.Printf("Error unmarshalling JSON: %v\n", err)
				d.Nack(false, false)
				continue
			}
			handler(payload)
			d.Ack(false)
		}
	}()
	return nil
}
