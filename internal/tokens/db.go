package tokens

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
)

func (tm *Manager) RecordBid(ctx context.Context, bid *Bid, cost int64, score float64) error {
	nowMilli := time.Now().UnixMilli()

	bidID := "bid_" + ksuid.New().String()

	br := &BidRow{
		Pk: GetBidPK(bid.TeamID),
		Sk: strings.Join(
			[]string{bid.TeamID, bidID, strconv.FormatInt(nowMilli, 10)},
			"#",
		),
		Target:      bid.UserID,
		Priority:    bid.Priority,
		Cost:        cost,
		Score:       score,
		CreatedAtMs: nowMilli,
		UpdatedAtMs: nowMilli,
	}

	brAv, err := attributevalue.MarshalMap(br)
	if err != nil {
		return err
	}

	_, err = tm.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableNameBids),
		Item:      brAv,
	})

	return nil
}

// Get token balance for a team
func (tm *Manager) GetTokenBalance(ctx context.Context, teamID string) (int64, int64, error) {
	result, err := tm.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableNameTokens),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: GetTokenPK(teamID)},
		},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("error fetching token balance: %v", err)
	}

	if result.Item == nil {
		return 0, 0, fmt.Errorf("team not found: %s", teamID)
	}

	var row TokenDBRow
	err = attributevalue.UnmarshalMap(result.Item, &row)
	if err != nil {
		return 0, 0, fmt.Errorf("error unmarshaling priority usage: %v", err)
	}

	return row.TokenBalance, row.ReputationScore, nil
}

// Simulate an auction for a user where teams bid tokens
func (tm *Manager) RunAuction(ctx context.Context, bids []Bid) (string, error) {
	var winningBid *Bid
	var winningBidCost int64
	var maxScore float64

	for _, bid := range bids {
		// get the team's current balance and reputation
		balance, reputation, err := tm.GetTokenBalance(ctx, bid.TeamID)
		if err != nil {
			return "", err
		}

		// rank the bid
		bidScore := calculateScore(bid.Priority, reputation)

		// check if team can afford the bid
		bidCost := tm.computeBidcost(&bid, reputation)

		// record the bid regardless of validity for record keeping
		err = tm.RecordBid(ctx, &bid, bidCost, bidScore)
		if err != nil {
			return "", err
		}

		if balance < bidCost {
			tm.logger.Warn(
				"team has insufficient tokens to bid",
				zap.String("team_id", bid.TeamID),
				zap.Int64("balance", balance),
				zap.Int64("bid_cost", bidCost),
			)
			continue
		}

		// if scores are equal, first score wins
		if bidScore > maxScore {
			maxScore = bidScore
			winningBid = &bid
			winningBidCost = bidCost
		}
	}

	if winningBid == nil {
		return "", fmt.Errorf("auction had no winner")
	}

	_, err := tm.SpendTokens(ctx, winningBid)
	if err != nil {
		return "", err
	}

	fmt.Printf(
		"Team %s won the auction for user %s with a bid of %d tokens\n",
		winningBid.TeamID,
		winningBid.UserID,
		winningBidCost,
	)
	return winningBid.TeamID, nil
}

// Refill tokens for all teams
func (tm *Manager) RefillTokens(ctx context.Context, teams []string) error {
	for _, teamID := range teams {
		_, err := tm.dynamoClient.UpdateItem(context.TODO(), &dynamodb.UpdateItemInput{
			TableName: aws.String(TableNameTokens),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: teamID},
			},
			UpdateExpression: aws.String(`
				SET token_balance = :initialBalance,
					reputation_score = :initialReputation
			`),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":initialBalance": &types.AttributeValueMemberN{
					Value: fmt.Sprintf("%d", InitialTokenCount),
				},
				":initialReputation": &types.AttributeValueMemberN{
					Value: fmt.Sprintf("%d", InitialReputationScore),
				},
			},
		})
		if err != nil {
			return fmt.Errorf("error refilling tokens for %s: %v", teamID, err)
		}
	}
	return nil
}

func (tm *Manager) GetBids(ctx context.Context, teamID string) ([]BidRow, error) {
	// Define the query input parameters
	input := &dynamodb.QueryInput{
		TableName:              aws.String("bids"), // The name of your table
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :skPrefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":       &types.AttributeValueMemberS{Value: GetBidPK(teamID)}, // Partition key
			":skPrefix": &types.AttributeValueMemberS{Value: teamID},           // Sort key prefix
		},
	}

	// Query the table
	result, err := tm.dynamoClient.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}

	// Unmarshal the results into a slice of Bid structs
	var bids []BidRow
	err = attributevalue.UnmarshalListOfMaps(result.Items, &bids)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal query result: %w", err)
	}

	return bids, nil
}
