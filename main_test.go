package main

import (
	"fmt"
	"sort"
)

func ExampleSteamURL_Match() {
	fmt.Println(SteamAppPage.Match("http://store.steampowered.com/app/477160"))
	fmt.Println(SteamAppPage.Match("http://store.steampowered.com/app/477160/Human_Fall_Flat/"))
	fmt.Println(SteamAppPage.Match("https://store.steampowered.com/app/477160"))
	fmt.Println(SteamAppPage.Match("https://store.steampowered.com/app/Human_Fall_Flat/"))
	// Output:
	// true
	// true
	// true
	// false
}

func ExampleSteamURL_Soup() {
	fmt.Printf("Getting name of app 477160 from %s:\n", SteamAppPage.Fill(477160))
	if soup, err := SteamAppPage.Soup(477160); err != nil {
		fmt.Printf("Could not get soup for %s, because %s", SteamAppPage.Fill(477160), err.Error())
	} else {
		fmt.Println(soup.Find("div", "id", "appHubAppName").Text())
	}
	// Output:
	// Getting name of app 477160 from https://store.steampowered.com/app/477160:
	// Human: Fall Flat
}

func ExampleSteamURL_JSON() {
	args := []any{477160, "*", "all", 20, "all", "all", -1, -1, "all"}
	fmt.Printf("Getting review stats for 477160 from %s:\n", SteamAppReviews.Fill(args...))
	if json, err := SteamAppReviews.JSON(args...); err != nil {
		fmt.Printf("Could not get reviews for %s, because %s", SteamAppReviews.Fill(args...), err.Error())
	} else {
		var i int
		keys := make([]string, len(json["query_summary"].(map[string]any)))
		for key := range json["query_summary"].(map[string]any) {
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
