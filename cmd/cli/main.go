package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	Version    = "0.2.0"
	InstallDir = "/opt/warp-proxy-manager"
)

var reader = bufio.NewReader(os.Stdin)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	// Lifecycle commands
	case "init":
		cmdInit()
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "uninstall":
		cmdUninstall()

	// API commands
	case "status":
		cmdStatus()
	case "containers":
		cmdContainers()
	case "scale":
		if len(args) < 1 {
			fmt.Println("Usage: warpctl scale <count>")
			os.Exit(1)
		}
		count, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("Invalid count: %s\n", args[0])
			os.Exit(1)
		}
		cmdScale(count)
	case "health":
		cmdHealth()
	case "metrics":
		cmdMetrics()
	case "create":
		cmdCreateContainer()
	case "restart":
		if len(args) < 1 {
			fmt.Println("Usage: warpctl restart <id>")
			os.Exit(1)
		}
		cmdRestart(args[0])
	case "delete":
		if len(args) < 1 {
			fmt.Println("Usage: warpctl delete <id>")
			os.Exit(1)
		}
		cmdDelete(args[0])
	case "history":
		cmdHistory()

	// Meta
	case "version", "-v", "--version":
		fmt.Printf("warpctl version %s\n", Version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`
=========================================
       WARP PROXY MANAGER
=========================================

Lifecycle:
  warpctl init             Interactive setup
  warpctl start            Start the manager
  warpctl stop             Stop the manager
  warpctl uninstall        Remove everything

Container:
  warpctl containers       List containers
  warpctl create           Create new container
  warpctl scale <n>        Scale to n containers
  warpctl restart <id>     Restart container
  warpctl delete <id>      Delete container

Monitor:
  warpctl status           Show manager status
  warpctl health           Show health status
  warpctl metrics          Show current metrics
  warpctl history          Show scale history

Options:
  -h, --help              Show help
  -v, --version           Show version`)
}

// Helper prompts
func prompt(question, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", question, defaultVal)
	} else {
		fmt.Printf("%s: ", question)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptYesNo(question string, defaultYes bool) bool {
	defaultVal := "y"
	if !defaultYes {
		defaultVal = "n"
	}
	fmt.Printf("%s (y/n) [%s]: ", question, defaultVal)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultYes
	}
	return input == "y" || input == "yes"
}

func promptChoice(question string, choices []string, defaultIdx int) string {
	fmt.Printf("\n%s\n", question)
	for i, c := range choices {
		marker := " "
		if i == defaultIdx {
			marker = ">"
		}
		fmt.Printf("  %s %d) %s\n", marker, i+1, c)
	}
	choice := prompt("Select", strconv.Itoa(defaultIdx+1))
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(choices) {
		return choices[defaultIdx]
	}
	return choices[idx-1]
}
