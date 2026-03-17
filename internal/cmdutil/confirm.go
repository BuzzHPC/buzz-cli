package cmdutil

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ConfirmDelete prints resource details and prompts the user to confirm deletion.
// Returns true if confirmed. Skips prompt if force is true.
func ConfirmDelete(force bool, resourceType, name string, details [][2]string) (bool, error) {
	if force {
		return true, nil
	}
	fmt.Printf("Delete %s %q?\n", resourceType, name)
	for _, d := range details {
		fmt.Printf("  %s: %s\n", d[0], d[1])
	}
	fmt.Print("Type 'yes' or 'y' to confirm: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false, nil
	}
	answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	return false, nil
}
