// Command ca-openamsm-plugin is the OpenAMSM SBI Event/Delivery Plugin. It
// implements only connectionagent.io/plugin-ipc/v1alpha1 and the frozen
// OpenAMSM Event contract; it is an ordinary standalone Go executable.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/odango-chan/ca-openamsm-plugin/internal/event"
	"github.com/odango-chan/ca-openamsm-plugin/internal/ipc"
	"github.com/odango-chan/ca-openamsm-plugin/internal/plugin"
)

func main() {
	classifier := event.Classifier(event.BlockedClassifier{})
	for _, argument := range os.Args[1:] {
		switch argument {
		case "serve":
			// Documented manifest argv.
		case "--version", "version":
			fmt.Printf("ca-openamsm-plugin %s (%s)\n", plugin.DefaultManifest.Version, ipc.APIVersion)
			return
		case "--test-only-classifier-double":
			// Test-only seam. The manifest never passes this flag; every report
			// that uses it must say AC-009-04 production binding is BLOCKED.
			fmt.Fprintln(os.Stderr, "ca-openamsm-plugin: TEST-ONLY classifier double enabled; production AC-009-04 remains BLOCKED_EXTERNAL_INPUT")
			classifier = event.TestDoubleClassifier{}
		default:
			fmt.Fprintf(os.Stderr, "ca-openamsm-plugin: unsupported argument %q (usage: ca-openamsm-plugin serve)\n", argument)
			os.Exit(2)
		}
	}

	instance := plugin.New(plugin.DefaultManifest, classifier, plugin.Options{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err := instance.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ca-openamsm-plugin: terminated: %v\n", err)
		var protocolError *ipc.Error
		if errors.As(err, &protocolError) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}
