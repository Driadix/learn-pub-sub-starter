package pubsub

import (
	"bytes"
	"context"
	"encoding/gob"
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

	queue, err := channel.QueueDeclare(queueName, isDurable, autoDelete, exclusive, false, amqp.Table{
		"x-dead-letter-exchange": "peril_dlx",
	})
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

type ACKType int

const (
	Ack ACKType = iota
	NackRequeue
	NackDiscard
)

func SubscribeJSON[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType,
	wg *sync.WaitGroup,
	handler func(T) ACKType,
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
			switch ackType := handler(payload); ackType {
			case Ack:
				d.Ack(false)
			case NackRequeue:
				d.Nack(false, true)
			case NackDiscard:
				d.Nack(false, false)
			}

		}
	}()
	return nil
}

func PublishGob[T any](ch *amqp.Channel, exchange, key string, val T) error {
	var buffer bytes.Buffer

	encoder := gob.NewEncoder(&buffer)
	err := encoder.Encode(val)
	if err != nil {
		return fmt.Errorf("Got an error encoding data to Gob")
	}

	err = ch.PublishWithContext(context.Background(), exchange, key, false, false, amqp.Publishing{
		ContentType: "application/gob",
		Body:        buffer.Bytes(),
	})
	if err != nil {
		return fmt.Errorf("Got an error publishing data: %v", err)
	}

	return nil
}

func SubscribeGob[T any](
	conn *amqp.Connection,
	exchange,
	queueName,
	key string,
	queueType SimpleQueueType,
	wg *sync.WaitGroup,
	handler func(T) ACKType,
) error {
	channel, _, err := DeclareAndBind(conn, exchange, queueName, key, queueType)
	if err != nil {
		fmt.Printf("Got an error creating and binding a queue: %v", err)
		return err
	}

	err = channel.Qos(10, 0, false)
	if err != nil {
		return fmt.Errorf("error setting Qos: %w", err)
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
			buffer := bytes.NewBuffer(d.Body)
			decoder := gob.NewDecoder(buffer)

			if err := decoder.Decode(&payload); err != nil {
				fmt.Printf("Error decoding payload: %v\n", err)
				d.Nack(false, false)
				continue
			}
			switch ackType := handler(payload); ackType {
			case Ack:
				d.Ack(false)
			case NackRequeue:
				d.Nack(false, true)
			case NackDiscard:
				d.Nack(false, false)
			}

		}
	}()
	return nil
}
