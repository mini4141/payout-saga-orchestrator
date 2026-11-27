// Command cancelfx is the Step Functions Lambda for the "cancelfx" step.
package main

import "github.com/mini4141/payout-saga-orchestrator/internal/lambdax"

func main() { lambdax.Start("cancelfx") }
