package email

import (
	"github.com/andygello555/game-scout/db/models"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/browser"
	"testing"
	"time"
)

func TestMeasureContext_Template(t *testing.T) {
	var err error
	for testNo, test := range []struct {
		context Context
		err     error
	}{
		{
			context: &MeasureContext{
				TrendingDevs: []*TrendingDev{
					{
						Developer: &models.Developer{
							Username: "testdev",
						},
						Snapshots: []*models.DeveloperSnapshot{
							{
								CreatedAt:                    time.Date(2009, 12, 1, 13, 0, 0, 0, time.UTC),
								Version:                      3,
								TimesHighlighted:             3,
								Tweets:                       10,
								LastTweetTime:                time.Date(2009, 11, 29, 13, 0, 0, 0, time.UTC),
								AverageDurationBetweenTweets: models.NullDurationFrom(time.Hour * 6),
								TweetsPublicMetrics: &twitter.TweetMetricsObj{
									Impressions:       0,
									URLLinkClicks:     3,
									UserProfileClicks: 5,
									Likes:             100,
									Replies:           10,
									Retweets:          5,
									Quotes:            0,
								},
								Games:                 2,
								GameWeightedScoresSum: 10000,
								WeightedScore:         12000,
							},
							{
								CreatedAt:                    time.Date(2009, 12, 5, 13, 0, 0, 0, time.UTC),
								Version:                      4,
								TimesHighlighted:             3,
								Tweets:                       10,
								LastTweetTime:                time.Date(2009, 11, 3, 13, 0, 0, 0, time.UTC),
								AverageDurationBetweenTweets: models.NullDurationFrom(time.Hour * 6),
								TweetsPublicMetrics: &twitter.TweetMetricsObj{
									Impressions:       0,
									URLLinkClicks:     3,
									UserProfileClicks: 5,
									Likes:             100,
									Replies:           10,
									Retweets:          5,
									Quotes:            0,
								},
								Games:                 2,
								GameWeightedScoresSum: 11000,
								WeightedScore:         13000,
							},
						},
						Games: []*models.Game{},
					},
				},
				TopSteamApps: nil,
			},
		},
	} {
		testNo = testNo + 1
		template := test.context.Template().HTML(test.context)
		if err = template.Error; err != nil {
			if test.err != nil {
				if test.err.Error() != err.Error() {
					t.Errorf("Expected error: %v for test no. %d, but got: %v", test.err, testNo, err)
				}
			} else {
				t.Errorf("Expected no error for test no. %d, but got: %v", testNo, err)
			}
		} else {
			_ = browser.OpenReader(&template.Buffer)
		}
	}
}
