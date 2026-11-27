// Command release is the Step Functions Lambda for the "release" step.
package main

import "github.com/mini4141/payout-saga-orchestrator/internal/lambdax"

func main() { lambdax.Start("release") }
