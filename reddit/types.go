package reddit

import (
	"encoding/json"
	"fmt"
	"github.com/anaskhan96/soup"
	"github.com/andygello555/game-scout/errors"
	"html"
	"strconv"
	"time"
)

const (
	kindComment           = "t1"
	kindUser              = "t2"
	kindPost              = "t3"
	kindMessage           = "t4"
	kindSubreddit         = "t5"
	kindTrophy            = "t6"
	kindListing           = "Listing"
	kindSubredditSettings = "subreddit_settings"
	kindKarmaList         = "KarmaList"
	kindTrophyList        = "TrophyList"
	kindUserList          = "UserList"
	kindMore              = "more"
	kindLiveThread        = "LiveUpdateEvent"
	kindLiveThreadUpdate  = "LiveUpdate"
	kindModAction         = "modaction"
	kindMulti             = "LabeledMulti"
	kindMultiDescription  = "LabeledMultiDescription"
	kindWikiPage          = "wikipage"
	kindWikiPageListing   = "wikipagelisting"
	kindWikiPageSettings  = "wikipagesettings"
	kindStyleSheet        = "stylesheet"
)

type TimePeriod string

const (
	Hour  TimePeriod = "hour"
	Day   TimePeriod = "day"
	Week  TimePeriod = "week"
	Month TimePeriod = "month"
	Year  TimePeriod = "year"
	All   TimePeriod = "all"
)

func (tp TimePeriod) Name() string {
	switch tp {
	case Hour:
		return "Hour"
	case Day:
		return "Day"
	case Week:
		return "Week"
	case Month:
		return "Month"
	case Year:
		return "Year"
	case All:
		return "All"
	default:
		return "<nil>"
	}
}

func (tp TimePeriod) String() string {
	return string(tp)
}

type Sort string

const (
	Hot           Sort = "hot"
	New           Sort = "new"
	Top           Sort = "top"
	Controversial Sort = "controversial"
)

func (s Sort) Name() string {
	switch s {
	case Hot:
		return "Hot"
	case New:
		return "New"
	case Top:
		return "Top"
	case Controversial:
		return "Controversial"
	default:
		return "<nil>"
	}
}

func (s Sort) String() string {
	return string(s)
}

type UserWhereType string

const (
	Overview  UserWhereType = "overview"
	Submitted UserWhereType = "submitted"
	Comments  UserWhereType = "comments"
	Upvoted   UserWhereType = "upvoted"
	Downvoted UserWhereType = "downvoted"
	Hidden    UserWhereType = "hidden"
	Saved     UserWhereType = "saved"
	Gilded    UserWhereType = "gilded"
)

func (uwt UserWhereType) Name() string {
	switch uwt {
	case Overview:
		return "Overview"
	case Submitted:
		return "Submitted"
	case Comments:
		return "Comments"
	case Upvoted:
		return "Upvoted"
	case Downvoted:
		return "Downvoted"
	case Hidden:
		return "Hidden"
	case Saved:
		return "Saved"
	case Gilded:
		return "Gilded"
	default:
		return "<nil>"
	}
}

func (uwt UserWhereType) String() string {
	return string(uwt)
}

// Timestamp represents a time that can be unmarshalled from a JSON string
// formatted as either an RFC3339 or Unix timestamp.
type Timestamp struct {
	time.Time
}

// MarshalJSON implements the json.Marshaler interface.
func (t *Timestamp) MarshalJSON() ([]byte, error) {
	if t == nil || t.Time.IsZero() {
		return []byte(`false`), nil
	}

	parsed := t.Time.Format(time.RFC3339)
	return []byte(`"` + parsed + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// Time is expected in RFC3339 or Unix format.
func (t *Timestamp) UnmarshalJSON(data []byte) (err error) {
	str := string(data)

	// "edited" for posts and comments is either false, or a timestamp.
	if str == "false" {
		return
	}

	f, err := strconv.ParseFloat(str, 64)
	if err == nil {
		t.Time = time.Unix(int64(f), 0).UTC()
	} else {
		t.Time, err = time.Parse(`"`+time.RFC3339+`"`, str)
	}

	return
}

// Equal reports whether t and u are equal based on time.Equal
func (t Timestamp) Equal(u Timestamp) bool {
	return t.Time.Equal(u.Time)
}

type Me struct {
	IsEmployee           bool   `json:"is_employee"`
	SeenLayoutSwitch     bool   `json:"seen_layout_switch"`
	HasVisitedNewProfile bool   `json:"has_visited_new_profile"`
	PrefNoProfanity      bool   `json:"pref_no_profanity"`
	HasExternalAccount   bool   `json:"has_external_account"`
	PrefGeopopular       string `json:"pref_geopopular"`
	SeenRedesignModal    bool   `json:"seen_redesign_modal"`
	PrefShowTrending     bool   `json:"pref_show_trending"`
	Subreddit            struct {
		DefaultSet                 bool   `json:"default_set"`
		UserIsContributor          bool   `json:"user_is_contributor"`
		BannerImg                  string `json:"banner_img"`
		RestrictPosting            bool   `json:"restrict_posting"`
		UserIsBanned               bool   `json:"user_is_banned"`
		FreeFormReports            bool   `json:"free_form_reports"`
		CommunityIcon              any    `json:"community_icon"`
		ShowMedia                  bool   `json:"show_media"`
		IconColor                  string `json:"icon_color"`
		UserIsMuted                any    `json:"user_is_muted"`
		DisplayName                string `json:"display_name"`
		HeaderImg                  any    `json:"header_img"`
		Title                      string `json:"title"`
		Coins                      int    `json:"coins"`
		PreviousNames              []any  `json:"previous_names"`
		Over18                     bool   `json:"over_18"`
		IconSize                   []int  `json:"icon_size"`
		PrimaryColor               string `json:"primary_color"`
		IconImg                    string `json:"icon_img"`
		Description                string `json:"description"`
		AllowedMediaInComments     []any  `json:"allowed_media_in_comments"`
		SubmitLinkLabel            string `json:"submit_link_label"`
		HeaderSize                 any    `json:"header_size"`
		RestrictCommenting         bool   `json:"restrict_commenting"`
		Subscribers                int    `json:"subscribers"`
		SubmitTextLabel            string `json:"submit_text_label"`
		IsDefaultIcon              bool   `json:"is_default_icon"`
		LinkFlairPosition          string `json:"link_flair_position"`
		DisplayNamePrefixed        string `json:"display_name_prefixed"`
		KeyColor                   string `json:"key_color"`
		Name                       string `json:"name"`
		IsDefaultBanner            bool   `json:"is_default_banner"`
		URL                        string `json:"url"`
		Quarantine                 bool   `json:"quarantine"`
		BannerSize                 any    `json:"banner_size"`
		UserIsModerator            bool   `json:"user_is_moderator"`
		AcceptFollowers            bool   `json:"accept_followers"`
		PublicDescription          string `json:"public_description"`
		LinkFlairEnabled           bool   `json:"link_flair_enabled"`
		DisableContributorRequests bool   `json:"disable_contributor_requests"`
		SubredditType              string `json:"subreddit_type"`
		UserIsSubscriber           bool   `json:"user_is_subscriber"`
	} `json:"subreddit"`
	PrefShowPresence    bool   `json:"pref_show_presence"`
	SnoovatarImg        string `json:"snoovatar_img"`
	SnoovatarSize       []int  `json:"snoovatar_size"`
	GoldExpiration      any    `json:"gold_expiration"`
	HasGoldSubscription bool   `json:"has_gold_subscription"`
	IsSponsor           bool   `json:"is_sponsor"`
	NumFriends          int    `json:"num_friends"`
	Features            struct {
		ModmailHarassmentFilter   bool `json:"modmail_harassment_filter"`
		ModServiceMuteWrites      bool `json:"mod_service_mute_writes"`
		PromotedTrendBlanks       bool `json:"promoted_trend_blanks"`
		ShowAmpLink               bool `json:"show_amp_link"`
		Chat                      bool `json:"chat"`
		IsEmailPermissionRequired bool `json:"is_email_permission_required"`
		ModAwards                 bool `json:"mod_awards"`
		ExpensiveCoinsPackage     bool `json:"expensive_coins_package"`
		MwebXpromoRevampV2        struct {
			Owner        string `json:"owner"`
			Variant      string `json:"variant"`
			ExperimentID int    `json:"experiment_id"`
		} `json:"mweb_xpromo_revamp_v2"`
		AwardsOnStreams                                    bool `json:"awards_on_streams"`
		MwebXpromoModalListingClickDailyDismissibleIos     bool `json:"mweb_xpromo_modal_listing_click_daily_dismissible_ios"`
		ChatSubreddit                                      bool `json:"chat_subreddit"`
		CookieConsentBanner                                bool `json:"cookie_consent_banner"`
		ModlogCopyrightRemoval                             bool `json:"modlog_copyright_removal"`
		ShowNpsSurvey                                      bool `json:"show_nps_survey"`
		DoNotTrack                                         bool `json:"do_not_track"`
		ImagesInComments                                   bool `json:"images_in_comments"`
		ModServiceMuteReads                                bool `json:"mod_service_mute_reads"`
		ChatUserSettings                                   bool `json:"chat_user_settings"`
		UsePrefAccountDeployment                           bool `json:"use_pref_account_deployment"`
		MwebXpromoInterstitialCommentsIos                  bool `json:"mweb_xpromo_interstitial_comments_ios"`
		MwebXpromoModalListingClickDailyDismissibleAndroid bool `json:"mweb_xpromo_modal_listing_click_daily_dismissible_android"`
		PremiumSubscriptionsTable                          bool `json:"premium_subscriptions_table"`
		MwebXpromoInterstitialCommentsAndroid              bool `json:"mweb_xpromo_interstitial_comments_android"`
		CrowdControlForPost                                bool `json:"crowd_control_for_post"`
		MwebSharingWebShareAPI                             struct {
			Owner        string `json:"owner"`
			Variant      string `json:"variant"`
			ExperimentID int    `json:"experiment_id"`
		} `json:"mweb_sharing_web_share_api"`
		ChatGroupRollout     bool `json:"chat_group_rollout"`
		ResizedStylesImages  bool `json:"resized_styles_images"`
		NoreferrerToNoopener bool `json:"noreferrer_to_noopener"`
	} `json:"features"`
	CanEditName             bool    `json:"can_edit_name"`
	Verified                bool    `json:"verified"`
	NewModmailExists        any     `json:"new_modmail_exists"`
	PrefAutoplay            bool    `json:"pref_autoplay"`
	Coins                   int     `json:"coins"`
	HasPaypalSubscription   bool    `json:"has_paypal_subscription"`
	HasSubscribedToPremium  bool    `json:"has_subscribed_to_premium"`
	ID                      string  `json:"id"`
	HasStripeSubscription   bool    `json:"has_stripe_subscription"`
	OauthClientID           string  `json:"oauth_client_id"`
	CanCreateSubreddit      bool    `json:"can_create_subreddit"`
	Over18                  bool    `json:"over_18"`
	IsGold                  bool    `json:"is_gold"`
	IsMod                   bool    `json:"is_mod"`
	AwarderKarma            int     `json:"awarder_karma"`
	SuspensionExpirationUtc any     `json:"suspension_expiration_utc"`
	HasVerifiedEmail        bool    `json:"has_verified_email"`
	IsSuspended             bool    `json:"is_suspended"`
	PrefVideoAutoplay       bool    `json:"pref_video_autoplay"`
	InChat                  bool    `json:"in_chat"`
	HasAndroidSubscription  bool    `json:"has_android_subscription"`
	InRedesignBeta          bool    `json:"in_redesign_beta"`
	IconImg                 string  `json:"icon_img"`
	HasModMail              bool    `json:"has_mod_mail"`
	PrefNightmode           bool    `json:"pref_nightmode"`
	AwardeeKarma            int     `json:"awardee_karma"`
	HideFromRobots          bool    `json:"hide_from_robots"`
	PasswordSet             bool    `json:"password_set"`
	LinkKarma               int     `json:"link_karma"`
	ForcePasswordReset      bool    `json:"force_password_reset"`
	TotalKarma              int     `json:"total_karma"`
	SeenGiveAwardTooltip    bool    `json:"seen_give_award_tooltip"`
	InboxCount              int     `json:"inbox_count"`
	SeenPremiumAdblockModal bool    `json:"seen_premium_adblock_modal"`
	PrefTopKarmaSubreddits  bool    `json:"pref_top_karma_subreddits"`
	HasMail                 bool    `json:"has_mail"`
	PrefShowSnoovatar       bool    `json:"pref_show_snoovatar"`
	Name                    string  `json:"name"`
	PrefClickgadget         int     `json:"pref_clickgadget"`
	Created                 float64 `json:"created"`
	GoldCreddits            int     `json:"gold_creddits"`
	CreatedUtc              float64 `json:"created_utc"`
	HasIosSubscription      bool    `json:"has_ios_subscription"`
	PrefShowTwitter         bool    `json:"pref_show_twitter"`
	InBeta                  bool    `json:"in_beta"`
	CommentKarma            int     `json:"comment_karma"`
	AcceptFollowers         bool    `json:"accept_followers"`
	HasSubscribed           bool    `json:"has_subscribed"`
	LinkedIdentities        []any   `json:"linked_identities"`
	SeenSubredditChatFtux   bool    `json:"seen_subreddit_chat_ftux"`
}

type listingWrapper struct {
	Data Listing `json:"data"`
	Kind string  `json:"kind"`
}

type Thing struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (t *Thing) UnmarshalJSON(b []byte) (err error) {
	root := new(struct {
		Kind string          `json:"kind"`
		Data json.RawMessage `json:"data"`
	})

	if err = json.Unmarshal(b, root); err != nil {
		return err
	}

	t.Kind = root.Kind
	var v any
	switch t.Kind {
	case kindListing:
		v = new(Listing)
	case kindPost:
		v = new(Post)
	case kindComment:
		v = new(Comment)
	case kindMore:
		v = new(More)
	default:
		err = fmt.Errorf("unrecognised kind %s", t.Kind)
		return
	}

	if err = json.Unmarshal(root.Data, v); err != nil {
		return err
	}
	t.Data = v
	return
}

type Things struct {
	Comments []*Comment
	Posts    []*Post
	Mores    []*More
}

func (t *Things) Merge(t2 *Things) {
	t.Comments = append(t.Comments, t2.Comments...)
	t.Posts = append(t.Posts, t2.Posts...)
	t.Mores = append(t.Mores, t2.Mores...)
}

func (t *Things) Len() int {
	return len(t.Comments) + len(t.Posts) + len(t.Mores)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (t *Things) UnmarshalJSON(b []byte) error {
	var things []Thing
	if err := json.Unmarshal(b, &things); err != nil {
		return err
	}

	t.add(things...)
	return nil
}

func (t *Things) add(things ...Thing) {
	for _, thing := range things {
		switch v := thing.Data.(type) {
		case *Post:
			t.Posts = append(t.Posts, v)
		case *Comment:
			t.Comments = append(t.Comments, v)
		case *More:
			t.Mores = append(t.Mores, v)
		}
	}
}

type Listings []Listing

func (l Listings) After() any {
	if len(l) > 0 {
		return l[len(l)-1].After()
	}
	return ""
}

type Listing struct {
	after     *string `json:"after"`
	Before    string  `json:"before"`
	Children  Things  `json:"children"`
	Dist      int     `json:"dist"`
	GeoFilter string  `json:"geo_filter"`
	Modhash   string  `json:"modhash"`
}

func (l *Listing) Len() int {
	return l.Children.Len()
}

func (l *Listing) After() any { return *l.after }

func (l *Listing) Merge(similar any) (err error) {
	if l2, ok := similar.(*Listing); !ok {
		return fmt.Errorf("cannot merge %T into Listing", similar)
	} else {
		l.Children.Merge(&l2.Children)
		l.after = l2.after
		l.Before = l2.Before
		l.Dist = l2.Dist
		l.GeoFilter = l2.GeoFilter
		l.Modhash = l2.Modhash
	}
	return
}

func (l *Listing) HasMore() bool { return l.after != nil }

// UnmarshalJSON implements the json.Unmarshaler interface.
func (l *Listing) UnmarshalJSON(b []byte) error {
	root := new(struct {
		After     *string `json:"after"`
		Before    string  `json:"before"`
		Children  Things  `json:"children"`
		Dist      int     `json:"dist"`
		GeoFilter string  `json:"geo_filter"`
		Modhash   string  `json:"modhash"`
	})

	err := json.Unmarshal(b, root)
	if err != nil {
		return err
	}

	l.after = root.After
	l.Before = root.Before
	l.Children = root.Children
	l.Dist = root.Dist
	l.GeoFilter = root.GeoFilter
	l.Modhash = root.Modhash

	return nil
}

// Post represents a post on a subreddit.
type Post struct {
	ID        string     `json:"id,omitempty"`
	FullID    string     `json:"name,omitempty"`
	Created   *Timestamp `json:"created_utc,omitempty"`
	Edited    *Timestamp `json:"edited,omitempty"`
	Permalink string     `json:"permalink,omitempty"`
	URL       string     `json:"url,omitempty"`
	Title     string     `json:"title,omitempty"`
	Body      string     `json:"selftext,omitempty"`
	BodyHTML  string     `json:"selftext_html,omitempty"`
	// Indicates if you've upvoted/downvoted (true/false).
	// If neither, it will be nil.
	Likes                 *bool   `json:"likes"`
	Ups                   int     `json:"ups"`
	Downs                 int     `json:"downs"`
	Score                 int     `json:"score"`
	UpvoteRatio           float32 `json:"upvote_ratio"`
	NumberOfComments      int     `json:"num_comments"`
	SubredditName         string  `json:"subreddit,omitempty"`
	SubredditNamePrefixed string  `json:"subreddit_name_prefixed,omitempty"`
	SubredditID           string  `json:"subreddit_id,omitempty"`
	SubredditSubscribers  int     `json:"subreddit_subscribers"`
	Author                string  `json:"author,omitempty"`
	AuthorID              string  `json:"author_fullname,omitempty"`
	Spoiler               bool    `json:"spoiler"`
	Locked                bool    `json:"locked"`
	NSFW                  bool    `json:"over_18"`
	IsSelfPost            bool    `json:"is_self"`
	Saved                 bool    `json:"saved"`
	Stickied              bool    `json:"stickied"`
}

func (p *Post) String() string {
	return fmt.Sprintf(
		`{ID: %v, FullID: %v, Created: %v, Edited: %v, Permalink: %v, URL: %v, Title: %v, Body: %v, Likes: %v, Ups: %v, Downs: %v, Score: %v, UpvoteRatio: %v, NumberOfComments: %v, SubredditName: %v, SubredditNamePrefixed: %v, SubredditID: %v, SubredditSubscribers: %v, Author: %v, AuthorID: %v, Spoiler: %v, Locked: %v, NSFW: %v, IsSelfPost: %v, Saved: %v, Stickied: %v}`,
		p.ID,
		p.FullID,
		p.Created,
		p.Edited,
		p.Permalink,
		p.URL,
		p.Title,
		p.Body,
		p.Likes,
		p.Ups,
		p.Downs,
		p.Score,
		p.UpvoteRatio,
		p.NumberOfComments,
		p.SubredditName,
		p.SubredditNamePrefixed,
		p.SubredditID,
		p.SubredditSubscribers,
		p.Author,
		p.AuthorID,
		p.Spoiler,
		p.Locked,
		p.NSFW,
		p.IsSelfPost,
		p.Saved,
		p.Stickied,
	)
}

func (p *Post) Soup() soup.Root {
	return soup.HTMLParse(html.UnescapeString(p.BodyHTML))
}

// Comment is a comment on a post.
type Comment struct {
	ID                    string     `json:"id,omitempty"`
	FullID                string     `json:"name,omitempty"`
	Created               *Timestamp `json:"created_utc,omitempty"`
	Edited                *Timestamp `json:"edited,omitempty"`
	ParentID              string     `json:"parent_id,omitempty"`
	Permalink             string     `json:"permalink,omitempty"`
	Body                  string     `json:"body,omitempty"`
	BodyHTML              string     `json:"body_html,omitempty"`
	Author                string     `json:"author,omitempty"`
	AuthorID              string     `json:"author_fullname,omitempty"`
	AuthorFlairText       string     `json:"author_flair_text,omitempty"`
	AuthorFlairID         string     `json:"author_flair_template_id,omitempty"`
	SubredditName         string     `json:"subreddit,omitempty"`
	SubredditNamePrefixed string     `json:"subreddit_name_prefixed,omitempty"`
	SubredditID           string     `json:"subreddit_id,omitempty"`
	// Indicates if you've upvote/downvoted (true/false).
	// If neither, it will be nil.
	Likes            *bool  `json:"likes"`
	Score            int    `json:"score"`
	Controversiality int    `json:"controversiality"`
	PostID           string `json:"link_id,omitempty"`
	// This doesn't appear consistently.
	PostTitle string `json:"link_title,omitempty"`
	// This doesn't appear consistently.
	PostPermalink string `json:"link_permalink,omitempty"`
	// This doesn't appear consistently.
	PostAuthor string `json:"link_author,omitempty"`
	// This doesn't appear consistently.
	PostNumComments *int    `json:"num_comments,omitempty"`
	IsSubmitter     bool    `json:"is_submitter"`
	ScoreHidden     bool    `json:"score_hidden"`
	Saved           bool    `json:"saved"`
	Stickied        bool    `json:"stickied"`
	Locked          bool    `json:"locked"`
	CanGild         bool    `json:"can_gild"`
	NSFW            bool    `json:"over_18"`
	Replies         Replies `json:"replies"`
}

func (c *Comment) Count() int {
	count := len(c.Replies.Comments)
	for _, reply := range c.Replies.Comments {
		count += reply.Count()
	}
	return count
}

func (c *Comment) Soup() soup.Root {
	return soup.HTMLParse(html.UnescapeString(c.BodyHTML))
}

// HasMore determines whether the comment has more replies to load in its reply tree.
func (c *Comment) HasMore() bool {
	return c.Replies.More != nil && len(c.Replies.More.Children) > 0
}

// addCommentToReplies traverses the comment tree to find the one
// that the 2nd comment is replying to. It then adds it to its replies.
func (c *Comment) addCommentToReplies(comment *Comment) {
	if c.FullID == comment.ParentID {
		c.Replies.Comments = append(c.Replies.Comments, comment)
		return
	}

	for _, reply := range c.Replies.Comments {
		reply.addCommentToReplies(comment)
	}
}

func (c *Comment) addMoreToReplies(more *More) {
	if c.FullID == more.ParentID {
		c.Replies.More = more
		return
	}

	for _, reply := range c.Replies.Comments {
		reply.addMoreToReplies(more)
	}
}

func (c *Comment) String() string {
	return fmt.Sprintf(
		`{ID: %v, FullID: %v, Created: %v, Edited: %v, ParentID: %v, Permalink: %v, Body: %v, BodyHTML: %v, Author: %v, AuthorID: %v, AuthorFlairText: %v, AuthorFlairID: %v, SubredditName: %v, SubredditNamePrefixed: %v, SubredditID: %v, Likes: %v, Score: %v, Controversiality: %v, PostID: %v, PostTitle: %v, PostPermalink: %v, PostAuthor: %v, PostNumComments: %v, IsSubmitter: %v, ScoreHidden: %v, Saved: %v, Stickied: %v, Locked: %v, CanGild: %v, NSFW: %v, Replies: %v}`,
		c.ID,
		c.FullID,
		c.Created,
		c.Edited,
		c.ParentID,
		c.Permalink,
		c.Body,
		c.BodyHTML,
		c.Author,
		c.AuthorID,
		c.AuthorFlairText,
		c.AuthorFlairID,
		c.SubredditName,
		c.SubredditNamePrefixed,
		c.SubredditID,
		c.Likes,
		c.Score,
		c.Controversiality,
		c.PostID,
		c.PostTitle,
		c.PostPermalink,
		c.PostAuthor,
		c.PostNumComments,
		c.IsSubmitter,
		c.ScoreHidden,
		c.Saved,
		c.Stickied,
		c.Locked,
		c.CanGild,
		c.NSFW,
		c.Replies,
	)
}

// Replies holds replies to a comment.
// It contains both comments and "more" comments, which are entrypoints to other
// comments that were left out.
type Replies struct {
	Comments []*Comment `json:"comments,omitempty"`
	More     *More      `json:"-"`
}

// UnmarshalJSON implements the json.Unmarshaler interface.
func (r *Replies) UnmarshalJSON(data []byte) error {
	// if a comment has no replies, its "replies" field is set to ""
	if string(data) == `""` {
		r = nil
		return nil
	}

	root := new(Thing)
	err := json.Unmarshal(data, root)
	if err != nil {
		return err
	}

	listing, _ := root.Data.(*Listing)

	r.Comments = listing.Children.Comments
	if len(listing.Children.Mores) > 0 {
		r.More = listing.Children.Mores[0]
	}
	return nil
}

// MarshalJSON implements the json.Marshaler interface.
func (r *Replies) MarshalJSON() ([]byte, error) {
	if r == nil || len(r.Comments) == 0 {
		return []byte(`null`), nil
	}
	return json.Marshal(r.Comments)
}

type moreResponse struct {
	JSON struct {
		Data struct {
			Things Things `json:"things"`
		} `json:"data"`
	} `json:"json"`
}

type commentResponse struct {
	more            *moreResponse
	postAndComments *PostAndComments
}

func (cr *commentResponse) UnmarshalJSON(data []byte) error {
	var pc PostAndComments
	err := json.Unmarshal(data, &pc)
	if err != nil {
		var more moreResponse
		if err2 := json.Unmarshal(data, &more); err2 != nil {
			return errors.MergeErrors(err2, err)
		}
		cr.more = &more
	} else {
		cr.postAndComments = &pc
	}
	return nil
}

// More holds information used to retrieve additional comments omitted from a base comment tree.
type More struct {
	ID       string `json:"id"`
	FullID   string `json:"name"`
	ParentID string `json:"parent_id"`
	// Count is the total number of replies to the parent + replies to those replies (recursively).
	Count int `json:"count"`
	// Depth is the number of comment nodes from the parent down to the furthest comment node.
	Depth    int      `json:"depth"`
	Children []string `json:"children"`
}

type PostsAndComments struct {
	Posts    []*Post
	Comments []*Comment
}

// PostAndComments is a post and its comments.
type PostAndComments struct {
	Post     *Post      `json:"post"`
	Comments []*Comment `json:"comments"`
	More     *More      `json:"-"`
}

func (pc *PostAndComments) After() any {
	if pc.HasMore() {
		return pc
	}
	return nil
}

// Merge for PostAndComments doesn't do anything because everything is already achieved in api.Binding.Response
func (pc *PostAndComments) Merge(postAndComments any) (err error) {
	return
}

func (pc *PostAndComments) Count() int {
	count := len(pc.Comments)
	for _, comment := range pc.Comments {
		count += comment.Count()
	}
	return count
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// When getting a sticky post, you get an array of 2 Listings
// The 1st one contains the single post in its children array
// The 2nd one contains the comments to the post
func (pc *PostAndComments) UnmarshalJSON(data []byte) error {
	var root [2]Thing

	err := json.Unmarshal(data, &root)
	if err != nil {
		return err
	}

	listing1 := root[0].Data.(*Listing)
	listing2 := root[1].Data.(*Listing)

	pc.Post = listing1.Children.Posts[0]
	pc.Comments = listing2.Children.Comments
	if len(listing2.Children.Mores) > 0 {
		pc.More = listing2.Children.Mores[0]
	}

	return nil
}

// HasMore determines whether the post has more replies to load in its reply tree.
func (pc *PostAndComments) HasMore() bool {
	return pc.More != nil && len(pc.More.Children) > 0
}

func (pc *PostAndComments) addCommentToTree(comment *Comment) {
	if pc.Post.FullID == comment.ParentID {
		pc.Comments = append(pc.Comments, comment)
		return
	}

	for _, reply := range pc.Comments {
		reply.addCommentToReplies(comment)
	}
}

func (pc *PostAndComments) addMoreToTree(more *More) {
	if pc.Post.FullID == more.ParentID {
		pc.More = more
	}

	for _, reply := range pc.Comments {
		reply.addMoreToReplies(more)
	}
}
