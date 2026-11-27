// Command reserve is the Step Functions Lambda for the "reserve" step.
package main

import "github.com/mini4141/payout-saga-orchestrator/internal/lambdax"

func main() { lambdax.Start("reserve") }
