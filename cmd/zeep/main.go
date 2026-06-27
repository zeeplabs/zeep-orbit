package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/zeep-tecnologia/zeep-core/internal/config"
	"github.com/zeep-tecnologia/zeep-core/internal/db"
	"github.com/zeep-tecnologia/zeep-core/internal/provisioner"
	"github.com/zeep-tecnologia/zeep-core/internal/registry"
	"github.com/zeep-tecnologia/zeep-core/internal/server"
)

var (
	configPath string
	dbOverride string
	port       int
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "zeep",
		Short: "zeep-core — Schema YAML → REST API automático",
	}

	rootCmd.PersistentFlags().StringVar(&configPath, "config", "./apps.yaml", "config file path")
	rootCmd.PersistentFlags().StringVar(&dbOverride, "db", "", "override DATABASE_URL")
	rootCmd.PersistentFlags().IntVar(&port, "port", 8080, "HTTP server port")

	rootCmd.AddCommand(cmdServe(), cmdApply(), cmdList(), cmdStatus())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdServe() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Inicia o servidor HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			if dbOverride != "" {
				cfg.Platform.DatabaseURL = dbOverride
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			pool, err := db.New(ctx, cfg.Platform.DatabaseURL)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			defer pool.Close()

			report, err := provisioner.New(pool).Apply(context.Background(), cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			printReport(report)

			reg := registry.New()
			if err := reg.Load(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			srv, err := server.New(reg, pool, port)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			if err := srv.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			return nil
		},
	}
}

func cmdApply() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "Provisiona schemas e tabelas no banco de dados",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			if dbOverride != "" {
				cfg.Platform.DatabaseURL = dbOverride
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			pool, err := db.New(ctx, cfg.Platform.DatabaseURL)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			defer pool.Close()

			report, err := provisioner.New(pool).Apply(context.Background(), cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			printReport(report)

			return nil
		},
	}
}

func cmdList() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista apps e tabelas carregados do config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			reg := registry.New()
			if err := reg.Load(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			apps := reg.Apps()

			// Ordenar por nome para saída determinística
			sort.Slice(apps, func(i, j int) bool {
				return apps[i].Config.Name < apps[j].Config.Name
			})

			for _, app := range apps {
				fmt.Println(app.Config.Name)

				// Ordenar tabelas por nome
				tableNames := make([]string, 0, len(app.Tables))
				for name := range app.Tables {
					tableNames = append(tableNames, name)
				}
				sort.Strings(tableNames)

				for _, tableName := range tableNames {
					fmt.Printf("  %s → http://localhost:%d/%s/%s\n",
						tableName, port, app.Config.Name, tableName)
				}
			}

			return nil
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
				fmt.Printf("Server not running at port %d\n", port)
				return nil
			}
			defer resp.Body.Close()

			var payload struct {
				Status string `json:"status"`
				Apps   int    `json:"apps"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				fmt.Fprintf(os.Stderr, "error: failed to parse health response: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("Status: %s\n", payload.Status)
			fmt.Printf("Apps: %d\n", payload.Apps)

			return nil
		},
	}
}

// printReport exibe o resultado do provisioner.Apply.
func printReport(report *provisioner.Report) {
	if len(report.SchemasCreated) == 0 && len(report.TablesCreated) == 0 && len(report.ColumnsAdded) == 0 {
		fmt.Println("  No changes")
		return
	}

	for _, schema := range report.SchemasCreated {
		fmt.Printf("✓ Created schema %s\n", schema)
	}
	for _, table := range report.TablesCreated {
		fmt.Printf("✓ Created table %s\n", table)
	}
	for _, col := range report.ColumnsAdded {
		fmt.Printf("✓ Added column %s\n", col)
	}
}
