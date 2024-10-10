package tokens

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/segmentio/ksuid"
	"go.uber.org/zap"
)

const (
	TableNameTokens        string = "tokens"
	TableNameBids          string = "bids"
	InitialTokenCount      int64  = 1000
	InitialReputationScore int64  = 100
)

var (
	// priority -> cost
	costMap = map[int64]int64{
		1:  1,
		2:  1,
		3:  1,
		4:  5,
		5:  5,
		6:  5,
		7:  7,
		8:  7,
		9:  7,
		10: 10,
	}

	InitialPriorityUsage = map[int]int{
		1:  0,
		2:  0,
		3:  0,
		4:  0,
		5:  0,
		6:  0,
		7:  0,
		8:  0,
		9:  0,
		10: 0,
	}
)

type Bid struct {
	TeamID   string
	UserID   string
	Priority int64
}

type Manager struct {
	dynamoClient *dynamodb.Client
	logger       *zap.Logger
}

type TokenDBRow struct {
	Pk              string      `dynamodbav:"pk"`
	TeamID          string      `dynamodbav:"team_id"`
	TokenBalance    int64       `dynamodbav:"token_balance"`
	LastRefillTime  int64       `dynamodbav:"last_refill_time"`
	ReputationScore int64       `dynamodbav:"reputation_score"`
	PriorityUsage   map[int]int `dynamodbav:"priority_usage"`
	CreatedAtMs     int64       `dynamodbav:"created_at_ms"`
	UpdatedAtMs     int64       `dynamodbav:"updated_at_ms"`
}

type BidRow struct {
	Pk          string  `dynamodbav:"pk"`
	Sk          string  `dynamodbav:"sk"`
	Target      string  `dynamodbav:"target"`
	Priority    int64   `dynamodbav:"priority"`
	Cost        int64   `dynamodbav:"cost"`
	Score       float64 `dynamodbav:"score"`
	CreatedAtMs int64   `dynamodbav:"created_at_ms"`
	UpdatedAtMs int64   `dynamodbav:"updated_at_ms"`
}

// Initialize DynamoDB Client
func NewManager() (*Manager, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config: %v", err)
	}
	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String("http://localhost:4566")
		o.Credentials = credentials.NewStaticCredentialsProvider("test", "test", "")
	})

	_, err = client.CreateTable(context.Background(), &dynamodb.CreateTableInput{
		TableName: aws.String("tokens"),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("pk"),
				KeyType:       types.KeyTypeHash,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("pk"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		zap.L().Warn("failed table create", zap.Error(err))
	} else {
		zap.L().Info("created tokens table")
	}

	_, err = client.CreateTable(context.Background(), &dynamodb.CreateTableInput{
		TableName: aws.String("bids"),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("pk"),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String("sk"),
				KeyType:       types.KeyTypeRange,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("pk"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("sk"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		zap.L().Warn("failed table create", zap.Error(err))
	} else {
		zap.L().Info("created bids table")
	}

	return &Manager{dynamoClient: client}, nil
}

// Initialize tokens for all teams
func (tm *Manager) InitializeTokens(ctx context.Context, teams []string) error {
	for _, teamID := range teams {

		now := time.Now().UnixMilli()
		item := &TokenDBRow{
			Pk:              GetTokenPK(teamID),
			TeamID:          teamID,
			TokenBalance:    InitialTokenCount,
			LastRefillTime:  now,
			ReputationScore: InitialReputationScore,
			PriorityUsage:   InitialPriorityUsage,
			CreatedAtMs:     now,
			UpdatedAtMs:     now,
		}

		itemAV, err := attributevalue.MarshalMap(item)
		if err != nil {
			return err
		}

		_, err = tm.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
			TableName:           aws.String(TableNameTokens),
			Item:                itemAV,
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		})
		if err != nil {
			var conditionCheckFailedErr *types.ConditionalCheckFailedException
			if ok := errors.As(err, &conditionCheckFailedErr); ok {
				fmt.Println("team already exists", teamID)
				continue
			}
			return fmt.Errorf("failed to initialize tokens for %s: %v", teamID, err)
		}
	}
	return nil
}

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

func (tm *Manager) computeBidcost(bid *Bid, reputation int64) int64 {
	minMultiplier := 1.0 // No price increase at max reputation
	maxMultiplier := 2.5 // 2.5x price increase at minimum reputation
	priceMultiplier := minMultiplier + (maxMultiplier-minMultiplier)*(1-float64(reputation)/100)

	cost := float64(costMap[bid.Priority]) * priceMultiplier

	return int64(cost)
}

// Spend tokens
func (tm *Manager) SpendTokens(
	ctx context.Context,
	bid *Bid,
) (int64, error) {
	balance, reputation, err := tm.GetTokenBalance(ctx, bid.TeamID)
	if err != nil {
		return 0, err
	}

	bidCost := tm.computeBidcost(bid, reputation)

	if balance < bidCost {
		return 0, fmt.Errorf("insufficient token balance: %d", balance)
	}

	// Update token balance
	// Increment priority utilization map
	output, err := tm.dynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(TableNameTokens),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: GetTokenPK(bid.TeamID)},
		},
		UpdateExpression: aws.String(`
			SET token_balance = token_balance - :amount,
				priority_usage.#usage_key = if_not_exists(priority_usage.#usage_key, :start) + :incr
		`),
		ConditionExpression: aws.String(
			"token_balance >= :amount",
		),
		ExpressionAttributeNames: map[string]string{
			"#usage_key": strconv.FormatInt(bid.Priority, 10),
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":amount": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", bidCost)},
			":incr":   &types.AttributeValueMemberN{Value: "1"},
			":start": &types.AttributeValueMemberN{
				Value: "0",
			},
		},
		ReturnValues: types.ReturnValueUpdatedNew,
	})
	if err != nil {
		return 0, fmt.Errorf("error updating token balance: %v", err)
	}

	// Check priority 10 usage and update reputation if necessary
	pum := output.Attributes["priority_usage"].(*types.AttributeValueMemberM)
	var priorityUsage map[int]int
	err = attributevalue.UnmarshalMap(pum.Value, &priorityUsage)
	if err != nil {
		return 0, fmt.Errorf("error parsing priority usage: %v", err)
	}

	if priorityUsage[10] > 5 {
		_, err := tm.dynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName: aws.String(TableNameTokens),
			Key: map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: GetTokenPK(bid.TeamID)},
			},
			UpdateExpression: aws.String(`
				SET reputation_score = reputation_score - :decrease
			`),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":decrease": &types.AttributeValueMemberN{Value: "10"}, // adjust decrease amount
			},
		})
		if err != nil {
			return 0, fmt.Errorf("error updating reputation score: %v", err)
		}
	}

	newBalance, err := strconv.ParseInt(
		output.Attributes["token_balance"].(*types.AttributeValueMemberN).Value,
		10,
		64,
	)
	if err != nil {
		return 0, fmt.Errorf("error parsing token balance: %v", err)
	}

	return newBalance, nil
}

func calculateScore(priority int64, reputation int64) float64 {
	var maxReputation int64 = 100

	// Normalize priority (1-10 scale)
	normalizedPriority := float64(priority-1) / 9.0 * 100.0

	// Normalize reputation (0-maxReputation scale)
	normalizedReputation := float64(reputation) / float64(maxReputation) * 100.0

	// Assign weights (70% priority, 30% reputation)
	score := (0.7 * normalizedPriority) + (0.3 * normalizedReputation)

	return score
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
