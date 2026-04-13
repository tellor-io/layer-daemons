// Command: go run ./scripts/keccak_queryids < hexlines
package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		b, err := hex.DecodeString(line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "decode: %v\n", err)
			continue
		}
		fmt.Println(hex.EncodeToString(crypto.Keccak256(b)))
	}
}
