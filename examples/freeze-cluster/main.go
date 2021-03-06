package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"go.universe.tf/virtuakube"
)

var (
	dir          = flag.String("universe-dir", "", "directory in which to place the universe")
	baseImg      = flag.String("vm-img", "virtuakube.qcow2", "VM base image")
	memory       = flag.Int("memory", 1024, "amount of memory per VM, in MiB")
	nodes        = flag.Int("nodes", 1, "number of worker nodes in addition to master")
	display      = flag.Bool("display", false, "create display windows for each VM")
	networkAddon = flag.String("network-addon", "calico", "network addon to install")
	verbose      = flag.Bool("verbose", false, "show commands being executed during cluster startup")
	kvm          = flag.Bool("kvm", true, "use KVM hardware acceleration")
)

func main() {
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	start := time.Now()

	if *dir == "" {
		return fmt.Errorf("-universe-dir is required (but will be created if non-existent")
	}

	cmd := virtuakube.Open
	_, err := os.Stat(*dir)
	if os.IsNotExist(err) {
		cmd = virtuakube.Create
	} else if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle ctrl+C by cancelling the context, which will shut down
	// everything in the universe.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()

	fmt.Println("Creating universe...")

	universe, err := cmd(ctx, *dir)
	if err != nil {
		return fmt.Errorf("Creating universe: %v", err)
	}
	defer universe.Close()

	cfg := &virtuakube.ClusterConfig{
		Name:     "freeze-example",
		NumNodes: *nodes,
		VMConfig: &virtuakube.VMConfig{
			Image:     *baseImg,
			MemoryMiB: *memory,
			NoKVM:     !*kvm,
		},
		NetworkAddon: *networkAddon,
	}
	if *verbose {
		cfg.VMConfig.CommandLog = os.Stdout
	}

	fmt.Println("Creating cluster...")

	cluster, err := universe.NewCluster(cfg)
	if err != nil {
		return fmt.Errorf("creating cluster: %v", err)
	}
	if err = cluster.Start(); err != nil {
		return fmt.Errorf("starting cluster: %v", err)
	}

	fmt.Println("Freezing universe...")

	if err := universe.Save(); err != nil {
		return fmt.Errorf("saving universe: %v", err)
	}

	fmt.Printf("Universe saved in %s. Use examples/thaw-universe to restore.\n", time.Since(start).Truncate(time.Second))

	return nil
}
