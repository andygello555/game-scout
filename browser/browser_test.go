package browser

import (
	"fmt"
	"github.com/playwright-community/playwright-go"
	"sort"
)

func ExampleNewBrowser() {
	browser, err := NewBrowser(true)
	if err != nil {
		fmt.Println("could not create Browser:", err)
	}
	if _, err = browser.Pages[0].Goto("https://github.com/playwright-community/playwright-go"); err != nil {
		fmt.Println("could not goto:", err)
	}
	var entry playwright.ElementHandle
	if entry, err = browser.Pages[0].QuerySelector("title"); err != nil {
		fmt.Println("could not get title:", err)
	}
	fmt.Println(entry.InnerText())
	if err = browser.Quit(); err != nil {
		fmt.Println("could not quit browser")
	}
	fmt.Println("Done!")
	// Output:
	// GitHub - playwright-community/playwright-go: Playwright for Go a browser automation library to control Chromium, Firefox and WebKit with a single API. <nil>
	// Done!
}

func ExampleScrapeURL_Regex() {
	fmt.Println(SteamAppPage.Regex())
	fmt.Println(ItchIOGamePage.Regex())
	// Output:
	// https?://store.steampowered.com/app/(\d+)
	// https?://([a-zA-Z0-9-._~]+).itch.io/([a-zA-Z0-9-._~]+)
}

func ExampleScrapeURL_Match() {
	fmt.Println(SteamAppPage.Match("http://store.steampowered.com/app/477160"))
	fmt.Println(SteamAppPage.Match("http://store.steampowered.com/app/477160/Human_Fall_Flat/"))
	fmt.Println(SteamAppPage.Match("https://store.steampowered.com/app/477160"))
	fmt.Println(SteamAppPage.Match("https://store.steampowered.com/app/Human_Fall_Flat/"))
	fmt.Println(ItchIOGamePage.Match("https://hempuli.itch.io/baba-files-taxes"))
	fmt.Println(ItchIOGamePage.Match("https://sokpop.itch.io/ballspell"))
	// Output:
	// true
	// true
	// true
	// false
	// true
	// true
}

func ExampleScrapeURL_ExtractArgs() {
	fmt.Println(SteamAppPage.ExtractArgs("http://store.steampowered.com/app/477160"))
	fmt.Println(SteamAppPage.ExtractArgs("https://store.steampowered.com/app/477160/Human_Fall_Flat/"))
	fmt.Println(ItchIOGamePage.ExtractArgs("https://hempuli.itch.io/baba-files-taxes"))
	fmt.Println(ItchIOGamePage.ExtractArgs("https://sokpop.itch.io/ballspell"))
	// Output:
	// [477160]
	// [477160]
	// [hempuli baba-files-taxes]
	// [sokpop ballspell]
}

func ExampleScrapeURL_Soup() {
	fmt.Printf("Getting name of app 477160 from %s:\n", SteamAppPage.Fill(477160))
	if soup, _, err := SteamAppPage.Soup(477160); err != nil {
		fmt.Printf("Could not get soup for %s, because %s", SteamAppPage.Fill(477160), err.Error())
	} else {
		fmt.Println(soup.Find("div", "id", "appHubAppName").Text())
	}
	// Output:
	// Getting name of app 477160 from https://store.steampowered.com/app/477160:
	// Human: Fall Flat
}

func ExampleScrapeURL_JSON() {
	args := []any{477160, "*", "all", 20, "all", "all", -1, -1, "all"}
	fmt.Printf("Getting review stats for 477160 from %s:\n", SteamAppReviews.Fill(args...))
	if j, _, err := SteamAppReviews.JSON(args...); err != nil {
		fmt.Printf("Could not get reviews for %s, because %s", SteamAppReviews.Fill(args...), err.Error())
	} else {
		var i int
		keys := make([]string, len(j["query_summary"].(map[string]any)))
		for key := range j["query_summary"].(map[string]any) {
			keys[i] = key
			i++
		}
		sort.Strings(keys)
		fmt.Println(keys)
	}
	// Output:
	// Getting review stats for 477160 from https://store.steampowered.com/appreviews/477160?json=1&cursor=*&language=all&day_range=9223372036854775807&num_per_page=20&review_type=all&purchase_type=all&filter=all&start_date=-1&end_date=-1&date_range_type=all:
	// [num_reviews review_score review_score_desc total_negative total_positive total_reviews]
}
