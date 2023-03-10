package reddit

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

type Listing struct {
	After     string         `json:"after"`
	Before    string         `json:"before"`
	Children  []ListingChild `json:"children"`
	Dist      int            `json:"dist"`
	GeoFilter string         `json:"geo_filter"`
	Modhash   string         `json:"modhash"`
}

type ListingChild struct {
	Data ListingData `json:"data"`
	Kind string      `json:"kind"`
}

type ListingGildings struct {
}

type RedditVideo struct {
	BitrateKbps       int    `json:"bitrate_kbps"`
	DashURL           string `json:"dash_url"`
	Duration          int    `json:"duration"`
	FallbackURL       string `json:"fallback_url"`
	Height            int    `json:"height"`
	HlsURL            string `json:"hls_url"`
	IsGif             bool   `json:"is_gif"`
	ScrubberMediaURL  string `json:"scrubber_media_url"`
	TranscodingStatus string `json:"transcoding_status"`
	Width             int    `json:"width"`
}

type ListingMedia struct {
	RedditVideo RedditVideo `json:"reddit_video"`
}

type ListingMediaEmbed struct {
}

type ListingSecureMedia struct {
	RedditVideo RedditVideo `json:"reddit_video"`
}

type ListingSecureMediaEmbed struct {
}

type ListingData struct {
	AllAwardings               []interface{}           `json:"all_awardings"`
	AllowLiveComments          bool                    `json:"allow_live_comments"`
	ApprovedAtUtc              interface{}             `json:"approved_at_utc"`
	ApprovedBy                 interface{}             `json:"approved_by"`
	Archived                   bool                    `json:"archived"`
	Author                     string                  `json:"author"`
	AuthorFlairBackgroundColor interface{}             `json:"author_flair_background_color"`
	AuthorFlairCSSClass        interface{}             `json:"author_flair_css_class"`
	AuthorFlairRichtext        []interface{}           `json:"author_flair_richtext"`
	AuthorFlairTemplateID      interface{}             `json:"author_flair_template_id"`
	AuthorFlairText            interface{}             `json:"author_flair_text"`
	AuthorFlairTextColor       interface{}             `json:"author_flair_text_color"`
	AuthorFlairType            string                  `json:"author_flair_type"`
	AuthorFullname             string                  `json:"author_fullname"`
	AuthorIsBlocked            bool                    `json:"author_is_blocked"`
	AuthorPatreonFlair         bool                    `json:"author_patreon_flair"`
	AuthorPremium              bool                    `json:"author_premium"`
	Awarders                   []interface{}           `json:"awarders"`
	BannedAtUtc                interface{}             `json:"banned_at_utc"`
	BannedBy                   interface{}             `json:"banned_by"`
	CanGild                    bool                    `json:"can_gild"`
	CanModPost                 bool                    `json:"can_mod_post"`
	Category                   interface{}             `json:"category"`
	Clicked                    bool                    `json:"clicked"`
	ContentCategories          interface{}             `json:"content_categories"`
	ContestMode                bool                    `json:"contest_mode"`
	Created                    float64                 `json:"created"`
	CreatedUtc                 float64                 `json:"created_utc"`
	DiscussionType             interface{}             `json:"discussion_type"`
	Distinguished              interface{}             `json:"distinguished"`
	Domain                     string                  `json:"domain"`
	Downs                      int                     `json:"downs"`
	Edited                     bool                    `json:"edited"`
	Gilded                     int                     `json:"gilded"`
	Gildings                   ListingGildings         `json:"gildings"`
	Hidden                     bool                    `json:"hidden"`
	HideScore                  bool                    `json:"hide_score"`
	ID                         string                  `json:"id"`
	IsCreatedFromAdsUI         bool                    `json:"is_created_from_ads_ui"`
	IsCrosspostable            bool                    `json:"is_crosspostable"`
	IsMeta                     bool                    `json:"is_meta"`
	IsOriginalContent          bool                    `json:"is_original_content"`
	IsRedditMediaDomain        bool                    `json:"is_reddit_media_domain"`
	IsRobotIndexable           bool                    `json:"is_robot_indexable"`
	IsSelf                     bool                    `json:"is_self"`
	IsVideo                    bool                    `json:"is_video"`
	Likes                      interface{}             `json:"likes"`
	LinkFlairBackgroundColor   string                  `json:"link_flair_background_color"`
	LinkFlairCSSClass          interface{}             `json:"link_flair_css_class"`
	LinkFlairRichtext          []interface{}           `json:"link_flair_richtext"`
	LinkFlairText              interface{}             `json:"link_flair_text"`
	LinkFlairTextColor         string                  `json:"link_flair_text_color"`
	LinkFlairType              string                  `json:"link_flair_type"`
	Locked                     bool                    `json:"locked"`
	Media                      ListingMedia            `json:"media"`
	MediaEmbed                 ListingMediaEmbed       `json:"media_embed"`
	MediaOnly                  bool                    `json:"media_only"`
	ModNote                    interface{}             `json:"mod_note"`
	ModReasonBy                interface{}             `json:"mod_reason_by"`
	ModReasonTitle             interface{}             `json:"mod_reason_title"`
	ModReports                 []interface{}           `json:"mod_reports"`
	Name                       string                  `json:"name"`
	NoFollow                   bool                    `json:"no_follow"`
	NumComments                int                     `json:"num_comments"`
	NumCrossposts              int                     `json:"num_crossposts"`
	NumReports                 interface{}             `json:"num_reports"`
	Over18                     bool                    `json:"over_18"`
	ParentWhitelistStatus      string                  `json:"parent_whitelist_status"`
	Permalink                  string                  `json:"permalink"`
	Pinned                     bool                    `json:"pinned"`
	Pwls                       int                     `json:"pwls"`
	Quarantine                 bool                    `json:"quarantine"`
	RemovalReason              interface{}             `json:"removal_reason"`
	RemovedBy                  interface{}             `json:"removed_by"`
	RemovedByCategory          interface{}             `json:"removed_by_category"`
	ReportReasons              interface{}             `json:"report_reasons"`
	Saved                      bool                    `json:"saved"`
	Score                      int                     `json:"score"`
	SecureMedia                ListingSecureMedia      `json:"secure_media"`
	SecureMediaEmbed           ListingSecureMediaEmbed `json:"secure_media_embed"`
	Selftext                   string                  `json:"selftext"`
	SelftextHTML               interface{}             `json:"selftext_html"`
	SendReplies                bool                    `json:"send_replies"`
	Spoiler                    bool                    `json:"spoiler"`
	Stickied                   bool                    `json:"stickied"`
	Subreddit                  string                  `json:"subreddit"`
	SubredditID                string                  `json:"subreddit_id"`
	SubredditNamePrefixed      string                  `json:"subreddit_name_prefixed"`
	SubredditSubscribers       int                     `json:"subreddit_subscribers"`
	SubredditType              string                  `json:"subreddit_type"`
	SuggestedSort              interface{}             `json:"suggested_sort"`
	Thumbnail                  string                  `json:"thumbnail"`
	Title                      string                  `json:"title"`
	TopAwardedType             interface{}             `json:"top_awarded_type"`
	TotalAwardsReceived        int                     `json:"total_awards_received"`
	TreatmentTags              []interface{}           `json:"treatment_tags"`
	Ups                        int                     `json:"ups"`
	UpvoteRatio                float64                 `json:"upvote_ratio"`
	URL                        string                  `json:"url"`
	URLOverriddenByDest        string                  `json:"url_overridden_by_dest"`
	UserReports                []interface{}           `json:"user_reports"`
	ViewCount                  interface{}             `json:"view_count"`
	Visited                    bool                    `json:"visited"`
	WhitelistStatus            string                  `json:"whitelist_status"`
	Wls                        int                     `json:"wls"`
}
