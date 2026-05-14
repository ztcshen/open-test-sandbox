package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"open-test-sandbox/internal/controlplane"
	"open-test-sandbox/internal/profile"
	"open-test-sandbox/internal/store/sqlite"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	switch os.Args[1] {
	case "version", "--version", "-v":
		fmt.Printf("Open Test Sandbox %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	case "store":
		if err := runStore(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "profile":
		if err := runProfile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printHelp()
		os.Exit(2)
	}
}

func printHelp() {
	fmt.Println(`Open Test Sandbox

Usage:
  otsandbox version
  otsandbox store status [--store-url PATH]
  otsandbox store migrate [--store-url PATH]
  otsandbox profile inspect --profile PATH
  otsandbox serve [--profile PATH] [--host HOST] [--port PORT]
  otsandbox help`)
}

func runStore(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing store command")
	}

	flags := flag.NewFlagSet("store "+args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	storeURL := flags.String("store-url", "", "SQLite store URL or path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	cfg := sqlite.ConfigFromURL(*storeURL)

	switch args[0] {
	case "status":
		status, err := sqlite.MigrationStatus(ctx, cfg)
		if err != nil {
			return err
		}
		printStoreStatus(status)
	case "migrate":
		status, err := sqlite.Migrate(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Migrated store to version %d\n", status.CurrentVersion)
		fmt.Printf("Applied: %d\n", status.AppliedCount)
		fmt.Printf("Path: %s\n", status.Path)
	default:
		return fmt.Errorf("unknown store command: %s", args[0])
	}
	return nil
}

func printStoreStatus(status sqlite.MigrationStatusResult) {
	pending := status.TargetVersion - status.CurrentVersion
	if pending < 0 {
		pending = 0
	}
	fmt.Println("Store: sqlite")
	fmt.Printf("Path: %s\n", status.Path)
	fmt.Printf("Version: %d\n", status.CurrentVersion)
	fmt.Printf("Target: %d\n", status.TargetVersion)
	fmt.Printf("Pending: %d\n", pending)
}

func runProfile(args []string) error {
	if len(args) == 0 {
		return errors.New("missing profile command")
	}

	flags := flag.NewFlagSet("profile "+args[0], flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "", "Profile bundle path")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}

	switch args[0] {
	case "inspect":
		bundle, err := profile.Load(*profilePath)
		if err != nil {
			return err
		}
		printProfile(bundle)
	default:
		return fmt.Errorf("unknown profile command: %s", args[0])
	}
	return nil
}

func printProfile(bundle profile.Bundle) {
	counts := bundle.Counts()
	fmt.Printf("Profile: %s\n", bundle.ID)
	fmt.Printf("Display Name: %s\n", bundle.DisplayName)
	fmt.Printf("Services: %d\n", counts.Services)
	fmt.Printf("Workflows: %d\n", counts.Workflows)
	fmt.Printf("Interface Nodes: %d\n", counts.InterfaceNodes)
	fmt.Printf("API Cases: %d\n", counts.APICases)
	fmt.Printf("Request Templates: %d\n", counts.RequestTemplates)
	fmt.Printf("Case Dependencies: %d\n", counts.CaseDependencies)
	fmt.Printf("Workflow Bindings: %d\n", counts.WorkflowBindings)
	fmt.Printf("Fixtures: %d\n", counts.Fixtures)
}

func runServe(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	profilePath := flags.String("profile", "profiles/empty", "Profile bundle path")
	host := flags.String("host", "127.0.0.1", "HTTP host")
	port := flags.Int("port", 18191, "HTTP port")
	if err := flags.Parse(args); err != nil {
		return err
	}

	bundle, err := profile.Load(*profilePath)
	if err != nil {
		return err
	}
	addr := *host + ":" + strconv.Itoa(*port)
	fmt.Printf("Open Test Sandbox listening on http://%s\n", addr)
	return http.ListenAndServe(addr, controlplane.New(bundle))
}
