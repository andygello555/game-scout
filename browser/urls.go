package browser

import "github.com/andygello555/url-fmt"

const (
	// SteamAppPage is the app page for an appid.
	SteamAppPage urlfmt.URL = "%s://store.steampowered.com/app/%d"
	// SteamAppDetails is the app details API from the Steam store. Can fetch the details, in JSON, for the given appid.
	SteamAppDetails urlfmt.URL = "%s://store.steampowered.com/api/appdetails/?appids=%d"
	// SteamAppReviews fetches a JSON object of the reviews for the given appid.
	SteamAppReviews urlfmt.URL = "%s://store.steampowered.com/appreviews/%d?json=1&cursor=%s&language=%s&day_range=9223372036854775807&num_per_page=%d&review_type=all&purchase_type=%s&filter=%s&start_date=%d&end_date=%d&date_range_type=%s"
	// SteamCommunityPosts fetches a JSON object of the required number of community posts by the partner (publisher).
	SteamCommunityPosts urlfmt.URL = "%s://store.steampowered.com/events/ajaxgetadjacentpartnerevents/?appid=%d&count_before=%d&count_after=%d&gidevent=%s&gidannouncement=%s&lang_list=0&origin=https://steamcommunity.com"
	// SteamGetAppList fetches a JSON object of all the names and IDs of the current apps on Steam.
	SteamGetAppList urlfmt.URL = "%s://api.steampowered.com/ISteamApps/GetAppList/v2/"
	// SteamSpyAppDetails is the URL for the app details API from Steamspy. Can fetch the details, in JSON, for the
	// given appid.
	SteamSpyAppDetails urlfmt.URL = "%s://steamspy.com/api.php?request=appdetails&appid=%d"
	// ItchIOGamePage is the game page for a given developer and game-title combo.
	ItchIOGamePage urlfmt.URL = "%s://%s.itch.io/%s"
	// ItchIOGameDevlogs is a page for an Itch.IO title listing all the developer logs/updates for that game. Like
	// ItchIOGamePage it also takes a developer and game-title slug.
	ItchIOGameDevlogs urlfmt.URL = "%s://%s.itch.io/%s/devlog"
	// ItchIOGameDevlog is a page for an individual devlog for the given developer-game slug pair for a title on
	// Itch.IO.
	ItchIOGameDevlog urlfmt.URL = "%s://%s.itch.io/%s/devlog/%d/%s"
)
