// Command bank is the Step Functions Lambda for the "bank" step.
package main

import "github.com/mini4141/payout-saga-orchestrator/internal/lambdax"

func main() { lambdax.Start("bank") }
