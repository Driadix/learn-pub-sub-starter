package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	pubsub "github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	fmt.Println("Starting Peril server...")
	connection, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		fmt.Println("Got an error starting amqp connection")
	}
	defer connection.Close()

	fmt.Println("Successfull connection to amqp")

	channel, err := connection.Channel()
	if err != nil {
		fmt.Println("Got an error creating a channel")
	}

	pubsub.PublishJSON(channel, routing.ExchangePerilDirect, routing.PauseKey, routing.PlayingState{
		IsPaused: true,
	})

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	<-signalChan
	fmt.Println("Shutdown signal received, initiating shutdown...")
	os.Exit(1)
}
