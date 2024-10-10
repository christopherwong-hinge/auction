# auction

`auction` is an experimental framework for managing a real-time auction for
sending Notifications to users. Each team is granted a number of tokens which
can be used to place bids on a specific user with a. Each bid is a transaction with
a corresponding priority (0-10, 10 being highest priority). After receiving
a bid, `auction` will calculate an associated cost of the bid, and if accepted,
deduct that balance from the user's token quota. As priority increases, the
associated cost of a bid also increases. `auction` also tracks the
frequency for which given priorities are requested by teams. This frequency
count is used to keep a running reputation score for a given team, in turn
affecting the cost function which determines the price of a bid.

## running locally

The included `docker-compose.yaml` will start a local instance of DynamoDB.

```bash
docker compose up
```

Running an auction:
```bash
go run cmd/auctiond/main.go
```

## auction process
1. All teams begin with a fixed allocation of `1000` tokens and a reputation
   score of `100`.
1. A team can bid on a given auction by specifying the targeted userID and
   a priority. The cost of a bid is calculated using the following function:

```
minMultiplier := 1.0       // No price increase at max reputation
maxMultiplier := 2.5       // 2.5x price increase at minimum reputation
priceMultiplier := minMultiplier + (maxMultiplier-minMultiplier)*(1-reputation)/100)
```
1. A team is eligible to bid if the cost of the bid is less than their current balance.
1. Bids are ranked according to a static weighting formula defined below.
1. The bid with the highest ranking wins and has the bid cost deducted from their balance.
1. The auction system also tracks a frequency count of priorities submitted by the bidding system.
1. If a team abuses a priority (more than 5 requests for priority 10 within a refill interval),
   their reputation score is penalized.

## ranking bids

This describes a basic weighed ranking system to evaluate a bid based on its priority and reputation.

1.	Normalize Priority:
We can map priority from its original range (1-10) to a 0-100 scale.

$$
\text{{Normalized Priority}} = \frac{{(\text{{Priority}} - 1)}}{{9}} \times 100
$$

2.	Normalize Reputation:
If the reputation already falls between 0-100, we can use it directly. If not, we normalize it based on the max possible reputation score, let’s say maxReputation.

$$
\text{{Normalized Reputation}} = \frac{{\text{{Reputation}}}}{{\text{{maxReputation}}}} \times 100
$$

3.	Weight the Components:
You can assign a weight to each factor. For example, priority might be more important than reputation, so we could assign a weight of 70% to priority and 30% to reputation.

$$
\text{{Combined Score}} = (0.7 \times \text{{Normalized Priority}}) + (0.3 \times \text{{Normalized Reputation}})
$$

### Example:
* priority = 9 (high priority)
* reputation = 50
* maxReputation = 100

The normalized values would be:

* Normalized Priority: $\frac{(9-1)}{9} \times 100 = 88.89$
* Normalized Reputation: $\frac{50}{100} \times 100 = 50$

Then, the combined score would be:

$\text{{Combined Score}} = (0.7 \times 88.89) + (0.3 \times 50) = 77.22$
