package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	fmt.Println("Starting Peril client...")
	connection, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		fmt.Println("Got an error starting amqp connection")
	}
	defer connection.Close()

	fmt.Println("Successfull connection to amqp")
	username, err := gamelogic.ClientWelcome()
	if err != nil {
		fmt.Println("Got an error while client Welcome operation")
	}

	queueName := routing.PauseKey + "." + username
	_, _, err = pubsub.DeclareAndBind(connection, routing.ExchangePerilDirect, queueName, routing.PauseKey, pubsub.TransientQueue)
	if err != nil {
		fmt.Printf("Got an error creating and binding a queue: %v", err)
	}

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	<-signalChan
	fmt.Println("Shutdown signal received, initiating shutdown...")
	os.Exit(1)
}
