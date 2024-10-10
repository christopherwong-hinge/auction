package main

import (
	"context"
	"time"

	"go.uber.org/zap"
	"golang.org/x/exp/rand"

	"github.com/christopherwong-hinge/auction/internal/tokens"
)

func main() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	zap.ReplaceGlobals(logger)

	r := rand.New(rand.NewSource(uint64(time.Now().UnixNano())))

	teams := []string{
		"2nCsmWUM2frXOp3HHceWJu75uxP",
		"2nCsmWs19fatOPEQaHTL8E1WcSu",
		"2nCsmWz9FpdfQCiEgzdImRAiuLt",
	}

	tm, err := tokens.NewManager()
	if err != nil {
		logger.Fatal("Failed to create token manager", zap.Error(err))
	}

	// Initialize tokens for all teams
	err = tm.InitializeTokens(context.TODO(), teams)
	if err != nil {
		logger.Fatal("Failed to initialize tokens", zap.Error(err))
	}

	bids := []tokens.Bid{
		{TeamID: teams[0], UserID: "123", Priority: int64(r.Intn(10) + 1)},
		{TeamID: teams[1], UserID: "123", Priority: int64(r.Intn(10) + 1)},
		{TeamID: teams[2], UserID: "123", Priority: int64(r.Intn(10) + 1)},
	}

	_ = bids

	// Run an auction for a user
	_, err = tm.RunAuction(context.TODO(), bids)
	if err != nil {
		logger.Fatal("Auction failed", zap.Error(err))
	}

	// Refill tokens for all teams
	// err = tm.RefillTokens(context.TODO(), teams)
	// if err != nil {
	// 	logger.Fatal("Failed to refill tokens", zap.Error(err))
	// }
}
