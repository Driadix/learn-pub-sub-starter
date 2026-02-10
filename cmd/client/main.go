package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func handlerPause(gs *gamelogic.GameState) func(routing.PlayingState) {
	return func(ps routing.PlayingState) {
		defer fmt.Print("> ")
		gs.HandlePause(ps)
	}
}

func handlerMove(gs *gamelogic.GameState) func(gamelogic.ArmyMove) {
	return func(am gamelogic.ArmyMove) {
		defer fmt.Print("> ")
		gs.HandleMove(am)
	}
}

func main() {
	fmt.Println("Starting Peril client...")

	var wg sync.WaitGroup

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		fmt.Println("\nShutdown signal received. Cleaning up...")
		fmt.Println("Goodbye!")
		wg.Wait()
		os.Exit(0)
	}()

	connection, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Got an error starting amqp connection: %v", err)
	}
	defer connection.Close()

	channel, err := connection.Channel()
	if err != nil {
		log.Fatalf("Could not open channel: %v", err)
	}

	fmt.Println("Successfull connection to amqp")
	username, err := gamelogic.ClientWelcome()
	if err != nil {
		log.Fatalf("Got an error while client Welcome operation: %v", err)
	}

	gameState := gamelogic.NewGameState(username)
	queueName := routing.PauseKey + "." + username
	err = pubsub.SubscribeJSON(connection, routing.ExchangePerilDirect, queueName, routing.PauseKey, pubsub.TransientQueue, &wg, handlerPause(gameState))
	if err != nil {
		log.Fatalf("Got an error subscribing: %v", err)
	}

	routingKey := routing.ArmyMovesPrefix + ".*"
	armyMoveQueueName := routing.ArmyMovesPrefix + "." + username
	err = pubsub.SubscribeJSON(connection, routing.ExchangePerilTopic, armyMoveQueueName, routingKey, pubsub.TransientQueue, &wg, handlerMove(gameState))
	if err != nil {
		log.Fatalf("Got an error subscribing: %v", err)
	}

	for {
		words := gamelogic.GetInput()
		if len(words) == 0 {
			continue
		}

		switch cmd := words[0]; cmd {
		case "spawn":
			err := gameState.CommandSpawn(words)
			if err != nil {
				fmt.Printf("An error occured spawning unit: %v", err)
			}
		case "move":
			move, err := gameState.CommandMove(words)
			if err != nil {
				fmt.Printf("An error occured spawning unit: %v", err)
			}
			err = pubsub.PublishJSON(channel, routing.ExchangePerilTopic, armyMoveQueueName, move)
			if err != nil {
				fmt.Printf("An error occured publishing move: %v", err)
			}
			fmt.Printf("Move %v was successfully published", move)
		case "status":
			gameState.CommandStatus()
		case "help":
			gamelogic.PrintClientHelp()
		case "spam":
			fmt.Printf("Spamming not allowed yet!\n")
		case "quit":
			gamelogic.PrintQuit()
			return
		default:
			fmt.Printf("Unknown command: %s\n", cmd)
			continue
		}

	}
}
