package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
	"github.com/zeeplabs/zeep-orbit/internal/dashboard"
	"github.com/zeeplabs/zeep-orbit/internal/db"
	"github.com/zeeplabs/zeep-orbit/internal/provisioner"
	"github.com/zeeplabs/zeep-orbit/internal/registry"
	"github.com/zeeplabs/zeep-orbit/internal/server"
)

var (
	dbOverride string
	port       int
)

func main() {
	_ = godotenv.Load()

	rootCmd := &cobra.Command{
		Use:   "zeep",
		Short: "zeep-orbit — self-hosted BaaS",
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

			brandTheme := os.Getenv("BRAND_THEME")
			if brandTheme == "" {
				brandTheme = "azure"
			}
			companyName := os.Getenv("BRAND_COMPANY_NAME")
			if companyName == "" {
				companyName = "Zeep Tecnologia"
			}
			if err := dashboard.SeedBrandConfig(context.Background(), pool, brandTheme, companyName); err != nil {
				fmt.Fprintf(os.Stderr, "error seeding brand config: %v\n", err)
				os.Exit(1)
			}

			reg := registry.New()
			if err := reg.LoadFromDB(context.Background(), pool); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}

			schemaNames := make([]string, 0, len(reg.Apps()))
			for _, app := range reg.Apps() {
				schemaNames = append(schemaNames, app.SchemaName)
			}
			// Grants zeep_app_enduser access to every existing app's schema.
			// Without this, apps provisioned before end-user-row-policies
			// shipped get "permission denied" on every end-user request,
			// since those now always run as zeep_app_enduser (see
			// db.Pool.WithRLSContext), not the schema-owning role. Warn
			// loudly instead of failing boot — one app with a stale/missing
			// schema shouldn't take every other app down with it.
			for _, err := range provisioner.New(pool).BackfillEnduserGrants(context.Background(), schemaNames) {
				fmt.Fprintf(os.Stderr, "WARNING: %v\n", err)
			}

			sysCfg, err := dashboard.GetSystemConfig(context.Background(), pool)
			if err != nil {
				// Fail-open would silently leave the registry at its zero-value
				// SystemConfig (StatementTimeoutMs: 0 = no timeout enforced,
				// SoftDeleteEnabled: false) without anyone noticing. Loud enough
				// to page on, since it means query-timeout protection is off.
				fmt.Fprintf(os.Stderr, "WARNING: failed to load system config at boot, statement_timeout and soft-delete enforcement are OFF until the next successful config read: %v\n", err)
			} else {
				reg.SetSystemConfig(registry.SystemConfig{
					SoftDeleteEnabled:  sysCfg.SoftDeleteEnabled,
					StatementTimeoutMs: sysCfg.StatementTimeoutMs,
				})
			}

			go func() {
				runPurge := func() {
					cfg, err := dashboard.GetSystemConfig(context.Background(), pool)
					if err == nil && cfg.RetentionDays > 0 && cfg.SoftDeleteEnabled {
						if _, err := dashboard.PurgeExpiredSoftDeletes(context.Background(), pool, reg, cfg.RetentionDays); err != nil {
							fmt.Fprintf(os.Stderr, "purge error: %v\n", err)
						}
					}
					// Webhook delivery log retention (inbound-webhooks
					// WEBHOOK-20): a fixed 30-day cutoff, independent of the
					// soft-delete config above — the delivery log purges
					// "regardless of a webhook's active/inactive/deleted
					// state" (spec.md Edge Cases), so it always runs on this
					// tick rather than being gated by SoftDeleteEnabled.
					if _, err := dashboard.PurgeExpiredDeliveries(context.Background(), pool, 30); err != nil {
						fmt.Fprintf(os.Stderr, "webhook delivery purge error: %v\n", err)
					}
				}
				// Run once at boot — otherwise a replica that restarts more
				// often than the 6h ticker period could go arbitrarily long
				// without ever purging.
				runPurge()
				ticker := time.NewTicker(6 * time.Hour)
				defer ticker.Stop()
				for range ticker.C {
					runPurge()
				}
			}()

			fmt.Printf("zeep-orbit starting on :%d (%d apps loaded)\n", port, len(reg.Apps()))

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
