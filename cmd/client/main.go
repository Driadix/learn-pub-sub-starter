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

func handlerPause(gs *gamelogic.GameState) func(routing.PlayingState) pubsub.ACKType {
	return func(ps routing.PlayingState) pubsub.ACKType {
		defer fmt.Print("> ")
		gs.HandlePause(ps)
		return pubsub.Ack
	}
}

func handlerMove(gs *gamelogic.GameState, ch *amqp.Channel) func(gamelogic.ArmyMove) pubsub.ACKType {
	return func(am gamelogic.ArmyMove) pubsub.ACKType {
		defer fmt.Print("> ")
		if outcome := gs.HandleMove(am); outcome == gamelogic.MoveOutComeSafe {
			return pubsub.Ack
		} else if outcome == gamelogic.MoveOutcomeMakeWar {
			warData := gamelogic.RecognitionOfWar{
				Attacker: am.Player,
				Defender: gs.GetPlayerSnap(),
			}
			routingKey := fmt.Sprintf("%s.%s", routing.WarRecognitionsPrefix, gs.GetPlayerSnap().Username)
			err := pubsub.PublishJSON(ch, routing.ExchangePerilTopic, routingKey, warData)
			if err != nil {
				fmt.Printf("error: failed to publish war recognition: %v\n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		}
		return pubsub.NackDiscard
	}
}

func handlerWar(gs *gamelogic.GameState) func(gamelogic.RecognitionOfWar) pubsub.ACKType {
	return func(row gamelogic.RecognitionOfWar) pubsub.ACKType {
		defer fmt.Print("> ")
		outcome, _, _ := gs.HandleWar(row)
		switch outcome {
		case gamelogic.WarOutcomeNotInvolved:
			return pubsub.NackRequeue
		case gamelogic.WarOutcomeNoUnits:
			return pubsub.NackDiscard
		case gamelogic.WarOutcomeOpponentWon:
			return pubsub.Ack
		case gamelogic.WarOutcomeYouWon:
			return pubsub.Ack
		case gamelogic.WarOutcomeDraw:
			return pubsub.Ack
		default:
			fmt.Printf("Got unknown war outcome: %v", outcome)
			return pubsub.NackDiscard
		}
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
	pauseQueueName := routing.PauseKey + "." + username
	err = pubsub.SubscribeJSON(connection, routing.ExchangePerilDirect, pauseQueueName, routing.PauseKey, pubsub.TransientQueue, &wg, handlerPause(gameState))
	if err != nil {
		log.Fatalf("Got an error subscribing to pause: %v", err)
	}

	armyRoutingKey := routing.ArmyMovesPrefix + ".*"
	armyMoveQueueName := routing.ArmyMovesPrefix + "." + username
	err = pubsub.SubscribeJSON(connection, routing.ExchangePerilTopic, armyMoveQueueName, armyRoutingKey, pubsub.TransientQueue, &wg, handlerMove(gameState, channel))
	if err != nil {
		log.Fatalf("Got an error subscribing to army: %v", err)
	}

	warRoutingKey := routing.WarRecognitionsPrefix + ".*"
	warQueueName := routing.WarRecognitionsPrefix
	err = pubsub.SubscribeJSON(connection, routing.ExchangePerilTopic, warQueueName, warRoutingKey, pubsub.DurableQueue, &wg, handlerWar(gameState))
	if err != nil {
		log.Fatalf("Got an error subscribing to war: %v", err)
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
