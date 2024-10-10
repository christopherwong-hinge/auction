package tokens

import "fmt"

func GetTokenPK(teamID string) string {
	return fmt.Sprintf("team#%s", teamID)
}

func GetBidPK(teamID string) string {
	return fmt.Sprintf("bid#%s", teamID)
}
