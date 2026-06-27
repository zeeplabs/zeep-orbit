package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/zeeplabs/zeep-core/internal/dashboard"
	"github.com/zeeplabs/zeep-core/internal/db"
	"github.com/zeeplabs/zeep-core/internal/registry"
	"github.com/zeeplabs/zeep-core/internal/server"
)

var (
	dbOverride string
	port       int
)

func main() {
	_ = godotenv.Load()

	rootCmd := &cobra.Command{
		Use:   "zeep",
		Short: "zeep-core — self-hosted BaaS",
	}

	rootCmd.PersistentFlags().StringVar(&dbOverride, "db", "", "override DATABASE_URL")
	rootCmd.PersistentFlags().IntVar(&port, "port", 8080, "HTTP server port")

	rootCmd.AddCommand(cmdServe(), cmdStatus())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdServe() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Inicia o servidor HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			dsn := dbOverride
			if dsn == "" {
				dsn = os.Getenv("DATABASE_URL")
			}
			if dsn == "" {
				fmt.Fprintln(os.Stderr, "error: DATABASE_URL not set")
				os.Exit(1)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			pool, err := db.New(ctx, dsn)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			defer pool.Close()

			if err := dashboard.ProvisionZeepSystem(context.Background(), pool); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			reg := registry.New()
			if err := reg.LoadFromDB(context.Background(), pool); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("zeep-core starting on :%d (%d apps loaded)\n", port, len(reg.Apps()))

			srv, err := server.New(reg, pool, port)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			return srv.Start()
		},
	}
}

func cmdStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verifica se o servidor está rodando",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := fmt.Sprintf("http://localhost:%d/health", port)
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				fmt.Printf("server not running at port %d\n", port)
				return nil
			}
			defer resp.Body.Close()
			fmt.Printf("status: %d\n", resp.StatusCode)
			return nil
		},
	}
}
