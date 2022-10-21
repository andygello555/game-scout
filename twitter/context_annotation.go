package twitter

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/g8rswimmer/go-twitter/v2"
	"github.com/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"strconv"
)

// ContextAnnotationDomain represents a domain that is used by the context annotations returned by the Twitter API for
// a tweet. This is defined so that we can weight each individual domain separately. For instance, VideoGame has a higher
// weighting in db.DeveloperSnapshot than VideoGamePublisher (as we are looking for unpublished games).
type ContextAnnotationDomain int

const (
	TvShows                     ContextAnnotationDomain = 3
	TvEpisodes                  ContextAnnotationDomain = 4
	SportsEvents                ContextAnnotationDomain = 6
	Person                      ContextAnnotationDomain = 10
	Sport                       ContextAnnotationDomain = 11
	SportsTeam                  ContextAnnotationDomain = 12
	Place                       ContextAnnotationDomain = 13
	TvGenres                    ContextAnnotationDomain = 22
	TvChannels                  ContextAnnotationDomain = 23
	SportsLeague                ContextAnnotationDomain = 26
	AmericanFootballGame        ContextAnnotationDomain = 27
	NflFootballGame             ContextAnnotationDomain = 28
	Events                      ContextAnnotationDomain = 29
	Community                   ContextAnnotationDomain = 31
	Politicians                 ContextAnnotationDomain = 35
	PoliticalRace               ContextAnnotationDomain = 38
	BasketballGame              ContextAnnotationDomain = 39
	SportsSeries                ContextAnnotationDomain = 40
	SoccerMatch                 ContextAnnotationDomain = 43
	BaseballGame                ContextAnnotationDomain = 44
	BrandVertical               ContextAnnotationDomain = 45
	BrandCategory               ContextAnnotationDomain = 46
	Brand                       ContextAnnotationDomain = 47
	Product                     ContextAnnotationDomain = 48
	Musician                    ContextAnnotationDomain = 54
	MusicGenre                  ContextAnnotationDomain = 55
	Actor                       ContextAnnotationDomain = 56
	EntertainmentPersonality    ContextAnnotationDomain = 58
	Athlete                     ContextAnnotationDomain = 60
	InterestsAndHobbiesVertical ContextAnnotationDomain = 65
	InterestsAndHobbiesCategory ContextAnnotationDomain = 66
	InterestsAndHobbies         ContextAnnotationDomain = 67
	HockeyGame                  ContextAnnotationDomain = 68
	VideoGame                   ContextAnnotationDomain = 71
	VideoGamePublisher          ContextAnnotationDomain = 78
	VideoGameHardware           ContextAnnotationDomain = 79
	CricketMatch                ContextAnnotationDomain = 83
	Book                        ContextAnnotationDomain = 84
	BookGenre                   ContextAnnotationDomain = 85
	Movie                       ContextAnnotationDomain = 86
	MovieGenre                  ContextAnnotationDomain = 87
	PoliticalBody               ContextAnnotationDomain = 88
	MusicAlbum                  ContextAnnotationDomain = 89
	RadioStation                ContextAnnotationDomain = 90
	Podcast                     ContextAnnotationDomain = 91
	SportsPersonality           ContextAnnotationDomain = 92
	Coach                       ContextAnnotationDomain = 93
	Journalist                  ContextAnnotationDomain = 94
	TvChannelEntityService      ContextAnnotationDomain = 95
	ReoccurringTrends           ContextAnnotationDomain = 109
	ViralAccounts               ContextAnnotationDomain = 110
	Concert                     ContextAnnotationDomain = 114
	VideoGameConference         ContextAnnotationDomain = 115
	VideoGameTournament         ContextAnnotationDomain = 116
	MovieFestival               ContextAnnotationDomain = 117
	AwardShow                   ContextAnnotationDomain = 118
	Holiday                     ContextAnnotationDomain = 119
	DigitalCreator              ContextAnnotationDomain = 120
	FictionalCharacter          ContextAnnotationDomain = 122
	MultimediaFranchise         ContextAnnotationDomain = 130
	UnifiedTwitterTaxonomy      ContextAnnotationDomain = 131
	VideoGamePersonality        ContextAnnotationDomain = 136
	ESportsTeam                 ContextAnnotationDomain = 137
	ESportsPlayer               ContextAnnotationDomain = 138
	FanCommunity                ContextAnnotationDomain = 139
	ESportsLeague               ContextAnnotationDomain = 149
	Food                        ContextAnnotationDomain = 152
	Weather                     ContextAnnotationDomain = 155
	Cities                      ContextAnnotationDomain = 156
	CollegesUniversities        ContextAnnotationDomain = 157
	PointsOfInterest            ContextAnnotationDomain = 158
	States                      ContextAnnotationDomain = 159
	Countries                   ContextAnnotationDomain = 160
	ExerciseFitness             ContextAnnotationDomain = 162
	Travel                      ContextAnnotationDomain = 163
	FieldsOfStudy               ContextAnnotationDomain = 164
	Technology                  ContextAnnotationDomain = 165
	Stocks                      ContextAnnotationDomain = 166
	Animals                     ContextAnnotationDomain = 167
	LocalNews                   ContextAnnotationDomain = 171
	GlobalTvShow                ContextAnnotationDomain = 172
	GoogleProductTaxonomy       ContextAnnotationDomain = 173
	DigitalAssetsCrypto         ContextAnnotationDomain = 174
	EmergencyEvents             ContextAnnotationDomain = 175
)

// String returns the name of the ContextAnnotationDomain. This is the same as the name value in the domain value of a
// context annotation returned by the Twitter API for a tweet.
func (cam ContextAnnotationDomain) String() string {
	switch cam {
	case TvShows:
		return "TV Shows"
	case TvEpisodes:
		return "TV Episodes"
	case SportsEvents:
		return "Sports Events"
	case Person:
		return "Person"
	case Sport:
		return "Sport"
	case SportsTeam:
		return "Sports Team"
	case Place:
		return "Place"
	case TvGenres:
		return "TV Genres"
	case TvChannels:
		return "TV Channels"
	case SportsLeague:
		return "Sports League"
	case AmericanFootballGame:
		return "American Football Game"
	case NflFootballGame:
		return "NFL Football Game"
	case Events:
		return "Events"
	case Community:
		return "Community"
	case Politicians:
		return "Politicians"
	case PoliticalRace:
		return "Political Race"
	case BasketballGame:
		return "Basketball Game"
	case SportsSeries:
		return "Sports Series"
	case SoccerMatch:
		return "Soccer Match"
	case BaseballGame:
		return "Baseball Game"
	case BrandVertical:
		return "Brand Vertical"
	case BrandCategory:
		return "Brand Category"
	case Brand:
		return "Brand"
	case Product:
		return "Product"
	case Musician:
		return "Musician"
	case MusicGenre:
		return "Music Genre"
	case Actor:
		return "Actor"
	case EntertainmentPersonality:
		return "Entertainment Personality"
	case Athlete:
		return "Athlete"
	case InterestsAndHobbiesVertical:
		return "Interests and Hobbies Vertical"
	case InterestsAndHobbiesCategory:
		return "Interests and Hobbies Category"
	case InterestsAndHobbies:
		return "Interests and Hobbies"
	case HockeyGame:
		return "Hockey Game"
	case VideoGame:
		return "Video Game"
	case VideoGamePublisher:
		return "Video Game Publisher"
	case VideoGameHardware:
		return "Video Game Hardware"
	case CricketMatch:
		return "Cricket Match"
	case Book:
		return "Book"
	case BookGenre:
		return "Book Genre"
	case Movie:
		return "Movie"
	case MovieGenre:
		return "Movie Genre"
	case PoliticalBody:
		return "Political Body"
	case MusicAlbum:
		return "Music Album"
	case RadioStation:
		return "Radio Station"
	case Podcast:
		return "Podcast"
	case SportsPersonality:
		return "Sports Personality"
	case Coach:
		return "Coach"
	case Journalist:
		return "Journalist"
	case TvChannelEntityService:
		return "TV Channel [Entity Service]"
	case ReoccurringTrends:
		return "Reoccurring Trends"
	case ViralAccounts:
		return "Viral Accounts"
	case Concert:
		return "Concert"
	case VideoGameConference:
		return "Video Game Conference"
	case VideoGameTournament:
		return "Video Game Tournament"
	case MovieFestival:
		return "Movie Festival"
	case AwardShow:
		return "Award Show"
	case Holiday:
		return "Holiday"
	case DigitalCreator:
		return "Digital Creator"
	case FictionalCharacter:
		return "Fictional Character"
	case MultimediaFranchise:
		return "Multimedia Franchise"
	case UnifiedTwitterTaxonomy:
		return "Unified Twitter Taxonomy"
	case VideoGamePersonality:
		return "Video Game Personality"
	case ESportsTeam:
		return "eSports Team"
	case ESportsPlayer:
		return "eSports Player"
	case FanCommunity:
		return "Fan Community"
	case ESportsLeague:
		return "Esports League"
	case Food:
		return "Food"
	case Weather:
		return "Weather"
	case Cities:
		return "Cities"
	case CollegesUniversities:
		return "Colleges & Universities"
	case PointsOfInterest:
		return "Points of Interest"
	case States:
		return "States"
	case Countries:
		return "Countries"
	case ExerciseFitness:
		return "Exercise & fitness"
	case Travel:
		return "Travel"
	case FieldsOfStudy:
		return "Fields of study"
	case Technology:
		return "Technology"
	case Stocks:
		return "Stocks"
	case Animals:
		return "Animals"
	case LocalNews:
		return "Local News"
	case GlobalTvShow:
		return "Global TV Show"
	case GoogleProductTaxonomy:
		return "Google Product Taxonomy"
	case DigitalAssetsCrypto:
		return "Digital Assets & Crypto"
	case EmergencyEvents:
		return "Emergency Events"
	default:
		return "<nil>"
	}
}

// Value returns the value of each domain in respect to their appropriateness towards Indie game posts on Twitter.
func (cam ContextAnnotationDomain) Value() float64 {
	switch cam {
	case VideoGamePublisher:
		return -500
	case Brand, Product, ViralAccounts, DigitalCreator, ESportsTeam, ESportsPlayer, ESportsLeague:
		return -5
	case TvChannels, RadioStation, Podcast, Events, Community, Person, BrandVertical, BrandCategory,
		EntertainmentPersonality, InterestsAndHobbiesVertical, ReoccurringTrends, MultimediaFranchise, FanCommunity,
		PointsOfInterest, FieldsOfStudy, Animals, LocalNews:
		return 5
	case InterestsAndHobbiesCategory, InterestsAndHobbies, VideoGameTournament, VideoGamePersonality:
		return 20
	case Technology, VideoGame:
		return 50
	case VideoGameHardware, VideoGameConference:
		return 500
	default:
		return 0.0
	}
}

// ContextAnnotationSet represents a set of ContextAnnotation.
type ContextAnnotationSet struct {
	mapset.Set[ContextAnnotation]
}

// Scan will first unmarshal the JSON bytes in the DB into an array of ContextAnnotation, from here a
// ContextAnnotationSet will be created, and the elements of the array will be added to this set.
func (cas *ContextAnnotationSet) Scan(value any) (err error) {
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("could not assert JSONB to bytes: \"%v\"", value)
	}

	var contextAnnotations []ContextAnnotation
	if err = json.Unmarshal(bytes, &contextAnnotations); err != nil {
		return errors.Wrap(err, fmt.Sprintf("could not unmarshal JSONB to ContextAnnotation \"%s\"", string(bytes)))
	}

	set := mapset.NewSet[ContextAnnotation]()
	for _, contextAnnotation := range contextAnnotations {
		set.Add(contextAnnotation)
	}
	*cas = ContextAnnotationSet{set}
	return
}

// Value marshals the ContextAnnotationSet to a JSON array of objects.
func (cas ContextAnnotationSet) Value() (driver.Value, error) {
	return json.Marshal(cas)
}

func (ContextAnnotationSet) GormDataType() string {
	return "jsonb"
}

func (ContextAnnotationSet) GormDBDataType(db *gorm.DB, field *schema.Field) string {
	switch db.Dialector.Name() {
	case "mysql", "sqlite":
		return "text"
	case "postgres":
		return "jsonb"
	}
	return ""
}

// NewContextAnnotationSet creates a ContextAnnotationSet. If twitter.TweetContextAnnotationObj objects are provided,
// these will be converted to ContextAnnotation using ContextAnnotation.FromTweetContextAnnotationObj, then added to the
// ContextAnnotationSet.
func NewContextAnnotationSet(objs []*twitter.TweetContextAnnotationObj) *ContextAnnotationSet {
	set := mapset.NewSet[ContextAnnotation]()
	for _, obj := range objs {
		ca := ContextAnnotation{}
		ca.FromTweetContextAnnotationObj(obj)
		set.Add(ca)
	}
	return &ContextAnnotationSet{set}
}

// ContextAnnotation represents a context annotation for a Tweet returned when executing a Binding.
type ContextAnnotation struct {
	Domain ContextAnnotationDomain `json:"domain"`
	Entity twitter.TweetContextObj `json:"entity"`
}

// FromTweetContextAnnotationObj sets the fields of the referred to ContextAnnotation using a
// twitter.TweetContextAnnotationObj.
func (ca *ContextAnnotation) FromTweetContextAnnotationObj(obj *twitter.TweetContextAnnotationObj) {
	domainID, _ := strconv.ParseInt(obj.Domain.ID, 10, 32)
	ca.Domain = ContextAnnotationDomain(domainID)
	ca.Entity = obj.Entity
}

// ToSet creates a mapset.Set for ContextAnnotation and pre-populates it with the referred to ContextAnnotation.
func (ca *ContextAnnotation) ToSet() *ContextAnnotationSet {
	set := mapset.NewSet[ContextAnnotation]()
	set.Add(*ca)
	return &ContextAnnotationSet{set}
}
