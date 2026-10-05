// File secret.go: generates a random shared secret for encrypted endpoints without embedding
// deployment credentials.

package secret

import (
	"crypto/rand"
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// Cmd registers the secret command, including its options and explicit error propagation.
var Cmd = &cobra.Command{
	Use:   "secret",
	Short: "Generates a secure, random 32-byte secret key",
	Long:  `This command generates a cryptographically secure 32-byte (256-bit) key and prints it. Use this key for the 'encryption.key' field in your config.yaml.`,
	Run: func(cmd *cobra.Command, args []string) {
		length := 32
		key := make([]byte, length)
		if _, err := rand.Read(key); err != nil {
			log.Fatalf("failed to generate random key: %v", err)
		}
		fmt.Printf("%x\n", key)
	},
}
