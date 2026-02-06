package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChan)

	<-signalChan
	fmt.Println("Shutdown signal received, initiating shutdown...")
	os.Exit(1)
}
