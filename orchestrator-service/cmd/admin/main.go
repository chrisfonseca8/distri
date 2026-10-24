// Command admin lists and revives sagas the sweeper marked STUCK.
//
//	go run ./cmd/admin stuck                 list STUCK sagas
//	go run ./cmd/admin revive <order_id>     send a stuck saga's current step again
//
// It connects with the same database settings as the service (DATABASE_TEST_URL).
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
	"uuid"

	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/admin"
	"github.com/chrisfonseca08/distri/orchestrator-service/pkg/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: admin stuck | admin revive <order_id>")
	}
	db, err := config.OpenDB()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx := context.Background()

	switch args[0] {
	case "stuck":
		rows, err := admin.ListStuck(ctx, db, 200)
		if err != nil {
			return err
		}
		writer := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
		fmt.Fprintln(writer, "ORDER\tSTEP\tSTEP STATUS\tATTEMPTS\tTOTAL\tSINCE\tLAST ERROR")
		for _, row := range rows {
			lastError := ""
			if row.LastError != nil {
				lastError = *row.LastError
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%d\t%d %s\t%s\t%s\n", row.ID, row.CurrentStep, row.StepStatus, row.Attempts,
				row.TotalPaise, row.Currency, row.UpdatedAt.Format(time.RFC3339), lastError)
		}
		writer.Flush()
		fmt.Printf("%d stuck saga(s)\n", len(rows))
		return nil
	case "revive":
		if len(args) != 2 {
			return fmt.Errorf("usage: admin revive <order_id>")
		}
		orderID, err := uuid.Parse(args[1])
		if err != nil {
			return fmt.Errorf("invalid order id: %w", err)
		}
		if err := admin.Revive(ctx, db, orderID, 15*time.Minute); err != nil {
			return err
		}
		fmt.Println("saga revived: its current step is due now and its deadline is 15 minutes away")
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
