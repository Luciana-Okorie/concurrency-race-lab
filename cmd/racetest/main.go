// racetest fires N concurrent debits against a single account and reports
// whether the database ended up in a correct state. Two modes:
//
//	naive - read-then-write, with a deliberate sleep between the read and
//	        the write to force the race window open. This should let more
//	        debits through than the balance can actually afford.
//	safe  - a single atomic conditional UPDATE. This should never let
//	        through more debits than the balance can afford, no matter how
//	        many goroutines race it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const accountName = "test-account"

func loadDotEnvIfPresent() {
	if os.Getenv("DATABASE_URL") != "" {
		return
	}
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
}

func main() {
	mode := flag.String("mode", "safe", "naive | safe")
	workers := flag.Int("workers", 50, "number of concurrent debit attempts")
	amount := flag.String("amount", "30", "amount per debit")
	reset := flag.Bool("reset", false, "reset the account balance to 1000.00 before running")
	flag.Parse()

	loadDotEnvIfPresent()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set (check .env)")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if *reset {
		if _, err := pool.Exec(ctx, `UPDATE accounts SET available = 1000.00 WHERE name = $1`, accountName); err != nil {
			log.Fatalf("reset: %v", err)
		}
	}

	var startBalance string
	if err := pool.QueryRow(ctx, `SELECT available FROM accounts WHERE name = $1`, accountName).Scan(&startBalance); err != nil {
		log.Fatalf("read starting balance: %v", err)
	}

	amountF, _ := strconv.ParseFloat(*amount, 64)
	startF, _ := strconv.ParseFloat(startBalance, 64)
	expectedMaxSuccesses := int(startF / amountF)

	var successCount int64
	var wg sync.WaitGroup

	fmt.Printf("mode=%s workers=%d amount=%s starting_balance=%s (max legitimate successes = %d)\n",
		*mode, *workers, *amount, startBalance, expectedMaxSuccesses)

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var ok bool
			var err error
			if *mode == "naive" {
				ok, err = naiveDebit(ctx, pool, *amount)
			} else {
				ok, err = safeDebit(ctx, pool, *amount)
			}
			if err != nil {
				log.Printf("debit error: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	var finalBalance string
	if err := pool.QueryRow(ctx, `SELECT available FROM accounts WHERE name = $1`, accountName).Scan(&finalBalance); err != nil {
		log.Fatalf("read final balance: %v", err)
	}
	finalF, _ := strconv.ParseFloat(finalBalance, 64)

	fmt.Printf("successful debits: %d (expected at most %d)\n", successCount, expectedMaxSuccesses)
	fmt.Printf("final balance: %s\n", finalBalance)

	switch {
	case finalF < 0:
		fmt.Println("RESULT: OVERSPEND - balance went negative. Race condition reproduced.")
	case int(successCount) > expectedMaxSuccesses:
		fmt.Println("RESULT: OVERSPEND - more debits succeeded than the balance could afford. Race condition reproduced.")
	default:
		fmt.Println("RESULT: CORRECT - balance and success count are consistent with the starting balance.")
	}
}

// naiveDebit is the anti-pattern: read, decide, sleep, then write. The sleep
// is what reliably forces two goroutines to both read a "sufficient"
// balance before either one writes.
func naiveDebit(ctx context.Context, pool *pgxpool.Pool, amount string) (bool, error) {
	var available string
	if err := pool.QueryRow(ctx, `SELECT available FROM accounts WHERE name = $1`, accountName).Scan(&available); err != nil {
		return false, err
	}

	availF, _ := strconv.ParseFloat(available, 64)
	amountF, _ := strconv.ParseFloat(amount, 64)
	if availF < amountF {
		return false, nil
	}

	time.Sleep(20 * time.Millisecond) // widen the race window on purpose

	if _, err := pool.Exec(ctx, `UPDATE accounts SET available = available - $1 WHERE name = $2`, amount, accountName); err != nil {
		return false, err
	}
	return true, nil
}

// safeDebit is the fix: one atomic statement does the check and the write.
func safeDebit(ctx context.Context, pool *pgxpool.Pool, amount string) (bool, error) {
	var remaining string
	err := pool.QueryRow(ctx, `
		UPDATE accounts
		SET available = available - $1
		WHERE name = $2 AND available >= $1
		RETURNING available
	`, amount, accountName).Scan(&remaining)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
