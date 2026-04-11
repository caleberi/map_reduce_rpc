package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/caleberi/map_reduce_rpc/mrp"
	"github.com/rs/zerolog"
)

func main() {
	logger := zerolog.New(os.Stdout).With().Caller().Timestamp().Logger()

	role := flag.String("role", "coordinator", "Service role: coordinator | master | worker")
	addr := flag.String("addr", "localhost:1234", "RPC listen address for selected role")
	dfsAddr := flag.String("dfs-address", envOrDefault("MAPREDUCE_DFS_SERVER_ADDRESS", "localhost:8089"), "DFS server address")
	masterAddr := flag.String("master-address", envOrDefault("MAPREDUCE_MASTER_SERVER_ADDRESS", "localhost:1235"), "Master RPC address")
	pluginPath := flag.String("plugin-path", envOrDefault("MAPREDUCE_PLUGIN_PATH", ""), "Worker plugin .so path")
	flag.Parse()

	if err := os.Setenv("MAPREDUCE_DFS_SERVER_ADDRESS", *dfsAddr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to set MAPREDUCE_DFS_SERVER_ADDRESS")
	}
	if err := os.Setenv("MAPREDUCE_MASTER_SERVER_ADDRESS", *masterAddr); err != nil {
		logger.Fatal().Err(err).Msg("Failed to set MAPREDUCE_MASTER_SERVER_ADDRESS")
	}
	if strings.TrimSpace(*pluginPath) != "" {
		if err := os.Setenv("MAPREDUCE_PLUGIN_PATH", *pluginPath); err != nil {
			logger.Fatal().Err(err).Msg("Failed to set MAPREDUCE_PLUGIN_PATH")
		}
	}

	switch strings.ToLower(strings.TrimSpace(*role)) {
	case "coordinator":
		coordinator, err := mrp.NewCoordinator(*addr, *dfsAddr)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to create coordinator")
		}
		defer func() {
			if err := coordinator.Close(); err != nil {
				logger.Error().Err(err).Msg("Failed to close coordinator cleanly")
			}
		}()

		if err := coordinator.Start(); err != nil {
			logger.Fatal().Err(err).Msg("Failed to start coordinator")
		}
		logger.Info().Str("role", "coordinator").Str("addr", *addr).Msg("Service is running")

	case "master":
		master, err := mrp.NewMaster(*addr)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to create master")
		}
		defer func() {
			if err := master.Close(); err != nil {
				logger.Error().Err(err).Msg("Failed to close master cleanly")
			}
		}()

		if err := master.Start(); err != nil {
			logger.Fatal().Err(err).Msg("Failed to start master")
		}
		logger.Info().Str("role", "master").Str("addr", *addr).Msg("Service is running")

	case "worker":
		worker, err := mrp.NewWorker(*addr)
		if err != nil {
			logger.Fatal().Err(err).Msg("Failed to create worker")
		}
		defer func() {
			if err := worker.Close(); err != nil {
				logger.Error().Err(err).Msg("Failed to close worker cleanly")
			}
		}()

		if err := worker.Start(); err != nil {
			logger.Fatal().Err(err).Msg("Failed to start worker")
		}
		logger.Info().Str("role", "worker").Str("addr", *addr).Str("master", *masterAddr).Msg("Service is running")

	default:
		logger.Fatal().Msg(fmt.Sprintf("Invalid role %q. Use coordinator, master, or worker", *role))
	}

	waitForShutdown()
}

func waitForShutdown() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
}

func envOrDefault(envKey, fallback string) string {
	value := os.Getenv(envKey)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
