// Command vapid prints a fresh Web Push VAPID key pair for the API
// environment. Regenerating it invalidates every stored subscription.
package main

import (
	"fmt"
	"os"

	"github.com/SherClockHolmes/webpush-go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}

	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", publicKey)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", privateKey)

	return nil
}
