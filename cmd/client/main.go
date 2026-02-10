package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/bootdotdev/learn-pub-sub-starter/internal/gamelogic"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/pubsub"
	"github.com/bootdotdev/learn-pub-sub-starter/internal/routing"
	amqp "github.com/rabbitmq/amqp091-go"
)

func publishGameLog(publishChannel *amqp.Channel, username, message string) error {
	gameLog := routing.GameLog{
		CurrentTime: time.Now(),
		Message:     message,
		Username:    username,
	}

	routingKey := routing.GameLogSlug + "." + username
	err := pubsub.PublishGob(publishChannel, routing.ExchangePerilTopic, routingKey, gameLog)
	if err != nil {
		return err
	}
	return nil
}

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

func handlerWar(gs *gamelogic.GameState, ch *amqp.Channel) func(gamelogic.RecognitionOfWar) pubsub.ACKType {
	return func(rw gamelogic.RecognitionOfWar) pubsub.ACKType {
		defer fmt.Print("> ")
		outcome, winner, loser := gs.HandleWar(rw)

		switch outcome {
		case gamelogic.WarOutcomeNotInvolved:
			return pubsub.NackRequeue
		case gamelogic.WarOutcomeNoUnits:
			return pubsub.NackDiscard
		case gamelogic.WarOutcomeOpponentWon, gamelogic.WarOutcomeYouWon, gamelogic.WarOutcomeDraw:
			var msg string
			if outcome == gamelogic.WarOutcomeDraw {
				msg = fmt.Sprintf("A war between %s and %s resulted in a draw", winner, loser)
			} else {
				msg = fmt.Sprintf("%s won a war against %s", winner, loser)
			}

			err := publishGameLog(ch, rw.Attacker.Username, msg)
			if err != nil {
				fmt.Printf("error: failed to publish game log: %v\n", err)
				return pubsub.NackRequeue
			}
			return pubsub.Ack
		default:
			fmt.Printf("error: unknown war outcome: %v\n", outcome)
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
	err = pubsub.SubscribeJSON(connection, routing.ExchangePerilTopic, warQueueName, warRoutingKey, pubsub.DurableQueue, &wg, handlerWar(gameState, channel))
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
			if len(words) < 2 {
				fmt.Printf("usage: spam <n>")
				continue
			}
			numberOfLogsStr := words[1]
			numberOfLogs, err := strconv.ParseInt(numberOfLogsStr, 10, 64)
			if numberOfLogs <= 0 {
				fmt.Printf("number of logs should be positive number")
				continue
			}
			if err != nil {
				fmt.Printf("An error occurred while converting str to int: %v", err)
				continue
			}
			for i := 0; i < int(numberOfLogs); i++ {
				maliciousLog := gamelogic.GetMaliciousLog()
				gameLogsRoutingKey := routing.GameLogSlug + "." + username
				err = pubsub.PublishGob(channel, routing.ExchangePerilTopic, gameLogsRoutingKey, routing.GameLog{
					CurrentTime: time.Now(),
					Username:    username,
					Message:     maliciousLog,
				})
				if err != nil {
					fmt.Printf("An error occured publishing malicious log: %v", err)
				}
			}
		case "quit":
			gamelogic.PrintQuit()
			return
		default:
			fmt.Printf("Unknown command: %s\n", cmd)
			continue
		}

	}
}
