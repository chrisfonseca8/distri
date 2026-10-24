// Command admin shows and resolves the payment work the automatic loops hand to a person.
//
//	go run ./cmd/admin review                       refunds waiting for a person
//	go run ./cmd/admin review-show <id>             one refund and its history
//	go run ./cmd/admin review-retry <id> "<note>"   send it to the worker as a new operation
//	go run ./cmd/admin parked                       webhook events that could not be applied
//	go run ./cmd/admin parked-replay <event_id>     queue one of them again
//	go run ./cmd/admin dead-jobs                    payment jobs that ran out of attempts
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

	"github.com/chrisfonseca08/distri/payment-service/pkg/admin"
	"github.com/chrisfonseca08/distri/payment-service/pkg/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func text(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: admin review | review-show <id> | review-retry <id> \"<note>\" | parked | parked-replay <event_id> | dead-jobs")
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
	writer := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	defer writer.Flush()

	parseID := func(position int) (uuid.UUID, error) {
		if len(args) <= position {
			return uuid.Nil(), fmt.Errorf("missing argument")
		}
		return uuid.Parse(args[position])
	}

	switch args[0] {
	case "review":
		rows, err := admin.ListReview(ctx, db, 200)
		if err != nil {
			return err
		}
		fmt.Fprintln(writer, "ID\tORDER\tGATEWAY\tPAYMENT\tAMOUNT\tREASON\tWHY IN REVIEW\tPROVIDER REFUND")
		for _, row := range rows {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%d %s\t%s\t%s\t%s\n", row.ID, row.OrderID, row.Gateway, row.GatewayPaymentID,
				row.AmountPaise, row.Currency, row.Reason, text(row.ReviewReason), text(row.RefundID))
		}
		fmt.Fprintf(writer, "%d refund(s) need a person\n", len(rows))
	case "review-show":
		id, err := parseID(1)
		if err != nil {
			return err
		}
		history, err := admin.History(ctx, db, id)
		if err != nil {
			return err
		}
		for _, entry := range history {
			fmt.Fprintf(writer, "%s\t%s -> %s\t%s\n", entry.At.Format(time.RFC3339), entry.FromStatus, entry.ToStatus, entry.Detail)
		}
	case "review-retry":
		id, err := parseID(1)
		if err != nil {
			return err
		}
		note := ""
		if len(args) > 2 {
			note = args[2]
		}
		if err := admin.RetryReview(ctx, db, id, note); err != nil {
			return err
		}
		fmt.Fprintln(writer, "sent back to the refund worker as a new operation")
	case "parked":
		rows, err := admin.ListParked(ctx, db, 200)
		if err != nil {
			return err
		}
		fmt.Fprintln(writer, "EVENT\tTYPE\tREASON\tGATEWAY ORDER\tRECEIVED\tLAST ERROR")
		for _, row := range rows {
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", row.EventID, row.EventType, text(row.ParkReason), text(row.GatewayOrderID),
				row.ReceivedAt.Format(time.RFC3339), text(row.LastError))
		}
		fmt.Fprintf(writer, "%d parked event(s)\n", len(rows))
	case "parked-replay":
		if len(args) != 2 {
			return fmt.Errorf("usage: admin parked-replay <event_id>")
		}
		if err := admin.ReplayParked(ctx, db, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(writer, "queued again")
	case "dead-jobs":
		rows, err := admin.ListDeadJobs(ctx, db, 200)
		if err != nil {
			return err
		}
		fmt.Fprintln(writer, "JOB\tPAYMENT\tATTEMPTS\tUPDATED\tLAST ERROR")
		for _, row := range rows {
			fmt.Fprintf(writer, "%s\t%s\t%d\t%s\t%s\n", row.ID, row.PaymentID, row.Attempts, row.UpdatedAt.Format(time.RFC3339), text(row.LastError))
		}
		fmt.Fprintf(writer, "%d dead job(s)\n", len(rows))
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}
