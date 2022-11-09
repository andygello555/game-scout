package browser

import (
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// ScrapeURL represents a page on Steam. It is usually a format string has string interpolation applied to it before
// fetching. The protocol should be given as a string verb at the beginning of the URL.
type ScrapeURL string

const (
	// SteamAppPage is the app page for an appid.
	SteamAppPage ScrapeURL = "%s://store.steampowered.com/app/%d"
	// SteamAppDetails is the app details API from the Steam store. Can fetch the details, in JSON, for the given appid.
	SteamAppDetails ScrapeURL = "%s://store.steampowered.com/api/appdetails/?appids=%d"
	// SteamAppReviews fetches a JSON object of the reviews for the given appid.
	SteamAppReviews ScrapeURL = "%s://store.steampowered.com/appreviews/%d?json=1&cursor=%s&language=%s&day_range=9223372036854775807&num_per_page=%d&review_type=all&purchase_type=%s&filter=%s&start_date=%d&end_date=%d&date_range_type=%s"
	// SteamCommunityPosts fetches a JSON object of the required number of community posts by the partner (publisher).
	SteamCommunityPosts ScrapeURL = "%s://store.steampowered.com/events/ajaxgetadjacentpartnerevents/?appid=%d&count_before=%d&count_after=%d&gidevent=%s&gidannouncement=%s&lang_list=0&origin=https://steamcommunity.com"
	// SteamSpyAppDetails is the URL for the app details API from Steamspy. Can fetch the details, in JSON, for the
	// given appid.
	SteamSpyAppDetails ScrapeURL = "%s://steamspy.com/api.php?request=appdetails&appid=%d"
)

// Name returns the name of the ScrapeURL.
func (su ScrapeURL) Name() string {
	switch su {
	case SteamAppPage:
		return "SteamAppPage"
	case SteamAppDetails:
		return "SteamAppDetails"
	case SteamAppReviews:
		return "SteamAppReviews"
	case SteamCommunityPosts:
		return "SteamCommunityPosts"
	case SteamSpyAppDetails:
		return "SteamSpyAppDetails"
	default:
		return "<nil>"
	}
}

// String returns the name of the ScrapeURL along with the URL in the format: "<Name>(<URL>)".
func (su ScrapeURL) String() string {
	return fmt.Sprintf("%s(%s)", su.Name(), string(su))
}

// Fill will apply string interpolation to the ScrapeURL. The protocol does not need to be included as "https" is always
// prepended to the args.
func (su ScrapeURL) Fill(args ...any) string {
	// We first prepend https onto the args so that we always fill out the protocol
	args = append([]any{"https"}, args...)
	return fmt.Sprintf(string(su), args...)
}

// Regex converts the ScrapeURL to a regex by replacing the string interpolation verbs with their regex character set
// counterparts.
func (su ScrapeURL) Regex() *regexp.Regexp {
	protocolString := regexp.MustCompile("%!([a-zA-Z])\\(MISSING\\)").ReplaceAllString(fmt.Sprintf(string(su), "https?"), "%$1")
	return regexp.MustCompile(regexp.MustCompile("%([a-zA-Z])").ReplaceAllString(protocolString, "(\\$1+)"))
}

// Match the given URL with a ScrapeURL to check if they are the same format.
func (su ScrapeURL) Match(url string) bool {
	return su.Regex().MatchString(url)
}

// ExtractArgs extracts the necessary arguments from the given URL to run the ScrapeURL.Soup, ScrapeURL.JSON, and
// ScrapeURL.Fill methods. This is useful when taking a URL matched by ScrapeURL.Match and fetching the soup for that
// matched URL.
func (su ScrapeURL) ExtractArgs(url string) (args []any) {
	pattern := su.Regex()
	metaPattern := regexp.MustCompile(`\(\\([a-zA-Z])\+\)`)
	groups := pattern.FindStringSubmatch(url)[1:]
	groupPatterns := metaPattern.FindStringSubmatch(pattern.String())[1:]
	if len(groups) != len(groupPatterns) {
		panic(fmt.Errorf(
			"the number of groups matched by %s doesn't match the number of groups found in the pattern (%d vs %d)",
			pattern.String(), len(groups), len(groupPatterns),
		))
	}
	args = make([]any, len(groups))
	for i, group := range groups {
		groupSet := groupPatterns[i]
		switch groupSet {
		case "d":
			args[i], _ = strconv.ParseInt(group, 10, 64)
		default:
			panic(fmt.Errorf("cannot parse character set \"%s\"", groupSet))
		}
	}
	return args
}

// Soup fetches the ScrapeURL and parses the returned HTML page into a soup.Root.
func (su ScrapeURL) Soup(args ...any) (*soup.Root, error) {
	url := su.Fill(args...)
	if resp, err := soup.Get(url); err != nil {
		return nil, errors.Wrapf(err, "could not get Steam page %s", url)
	} else {
		doc := soup.HTMLParse(resp)
		return &doc, nil
	}
}

// TrySoup will run Soup with the given args and try the given function. If the function returns an error then the
// function will be retried up to a total of the given number of maxTries. If minDelay is given, and is not 0, then
// before the function is retried it will sleep for (maxTries + 1 - currentTries) * minDelay.
func (su ScrapeURL) TrySoup(maxTries int, minDelay time.Duration, try func(doc *soup.Root) error, args ...any) (err error) {
	tries := maxTries
	type returnType int
	const (
		con returnType = iota
		brk
		done
	)
	for {
		rt := func() (rt returnType) {
			log.INFO.Printf("TrySoup(%s, %v) is on try %d/%d", su.Name(), args, tries, maxTries)
			// Defer a closure to recover from any panics and set the appropriate return type
			defer func() {
				if pan := recover(); pan != nil {
					if tries > 0 {
						tries--
						time.Sleep(minDelay * time.Duration(maxTries+1-tries))
						rt = con
						return
					}
					err = fmt.Errorf("panic occurred: %v", pan)
					rt = brk
				}
			}()

			var doc *soup.Root
			if doc, err = su.Soup(args...); err != nil {
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return con
				}
				err = errors.Wrapf(err, "ran out of tries (%d total) whilst requesting Soup for %s", maxTries, su.String())
				return brk
			}

			if err = try(doc); err != nil {
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return con
				}
				err = errors.Wrapf(err, "ran out of tries (%d total) whilst calling try function for %s", maxTries, su.String())
				return brk
			}
			return done
		}()

		if rt == con {
			continue
		}
		break
	}
	return
}

// JSON makes a request to the ScrapeURL and parses the response to JSON.
func (su ScrapeURL) JSON(args ...any) (jsonBody map[string]any, err error) {
	url := su.Fill(args...)
	client := http.Client{Timeout: time.Second * 10}

	var req *http.Request
	if req, err = http.NewRequest(http.MethodGet, url, nil); err != nil {
		err = errors.Wrapf(err, "request for JSON for \"%s\" could not be created", url)
		return
	}

	var res *http.Response
	if res, err = client.Do(req); err != nil {
		err = errors.Wrapf(err, "JSON could not be fetched from \"%s\"", url)
		return
	}

	if res.Body != nil {
		defer func(Body io.ReadCloser) {
			if err = Body.Close(); err != nil {
				err = errors.Wrapf(err, "request body for JSON fetched from \"%s\" could not be closed", url)
			}
		}(res.Body)
	}

	var body []byte
	if body, err = io.ReadAll(res.Body); err != nil {
		err = errors.Wrapf(err, "JSON request body from \"%s\" could not be read", url)
		return
	}

	jsonBody = make(map[string]any)
	if err = json.Unmarshal(body, &jsonBody); err != nil {
		err = errors.Wrapf(err, "JSON could not be parsed from response from \"%s\"", url)
		return
	}
	return
}

// TryJSON will run JSON with the given args and try the given function. If the function returns an error then the
// function will be retried up to a total of the given number of maxTries. If minDelay is given, and is not 0, then
// before the function is retried it will sleep for (maxTries + 1 - currentTries) * minDelay.
func (su ScrapeURL) TryJSON(maxTries int, minDelay time.Duration, try func(jsonBody map[string]any) error, args ...any) (err error) {
	tries := maxTries
	type returnType int
	const (
		con returnType = iota
		brk
		done
	)
	for {
		rt := func() (rt returnType) {
			log.INFO.Printf("TryJSON(%s, %v) is on try %d/%d", su.Name(), args, tries, maxTries)
			// Defer a closure to recover from any panics and set the appropriate return type
			defer func() {
				if pan := recover(); pan != nil {
					if tries > 0 {
						tries--
						time.Sleep(minDelay * time.Duration(maxTries+1-tries))
						rt = con
						return
					}
					err = fmt.Errorf("panic occurred: %v", pan)
					rt = brk
				}
			}()

			var jsonBody map[string]any
			if jsonBody, err = su.JSON(args...); err != nil {
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return con
				}
				err = errors.Wrapf(err, "ran out of tries (%d total) whilst requesting JSON for %s", maxTries, su.String())
				return brk
			}

			if err = try(jsonBody); err != nil {
				if tries > 0 {
					tries--
					time.Sleep(minDelay * time.Duration(maxTries+1-tries))
					return con
				}
				err = errors.Wrapf(err, "ran out of tries (%d total) whilst calling try function for %s", maxTries, su.String())
				return brk
			}
			return done
		}()

		if rt == con {
			continue
		}
		break
	}
	return
}
