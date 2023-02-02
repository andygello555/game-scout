package browser

import (
	"encoding/json"
	"fmt"
	"github.com/RichardKnop/machinery/v1/log"
	"github.com/anaskhan96/soup"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type verb string

const (
	// stringVerb: the uninterpreted bytes of the string or slice
	stringVerb verb = "s"
	// boolVerb: the word true or false
	boolVerb verb = "t"
	// base2Verb: base 2
	base2Verb verb = "b"
	// charVerb: the character represented by the corresponding Unicode code point
	charVerb verb = "c"
	// base8Verb: base 8
	base8Verb verb = "o"
	// base8PrefixVerb: base 8 with 0o prefix
	base8PrefixVerb verb = "O"
	// base10Verb: base 10
	base10Verb verb = "d"
	// unicodeVerb: Unicode format: U+1234; same as "U+%04X"
	unicodeVerb verb = "U"
	// scientificNotationLowerVerb: scientific notation, e.g. -1.234456e+78
	scientificNotationLowerVerb verb = "e"
	// scientificNotationUpperVerb: scientific notation, e.g. -1.234456E+78
	scientificNotationUpperVerb verb = "E"
	// floatVerb: decimal point but no exponent, e.g. 123.456
	floatVerb verb = "f"
	// floatSynonymVerb: synonym for %f
	floatSynonymVerb verb = "F"
	// floatHexLowerVerb: hexadecimal notation (with decimal power of two exponent), e.g. -0x1.23abcp+20
	floatHexLowerVerb verb = "x"
	// floatHexUpperVerb: upper-case hexadecimal notation, e.g. -0X1.23ABCP+20
	floatHexUpperVerb verb = "X"
)

type verbRegexPattern string

const (
	// stringVerbRegexPattern: the uninterpreted bytes of the string or slice
	stringVerbRegexPattern verbRegexPattern = `([a-zA-Z0-9-._~]+)`
	// boolVerbRegexPattern: the word true or false
	boolVerbRegexPattern verbRegexPattern = `(true|false)`
	// base2VerbRegexPattern: base 2
	base2VerbRegexPattern verbRegexPattern = `([01]+)`
	// charVerbRegexPattern: the character represented by the corresponding Unicode code point
	charVerbRegexPattern verbRegexPattern = `(.)`
	// base8VerbRegexPattern: base 8
	base8VerbRegexPattern verbRegexPattern = `([0-7]+)`
	// base8PrefixVerbRegexPattern: base 8 with 0o prefix
	base8PrefixVerbRegexPattern verbRegexPattern = `(0o[0-7]+)`
	// base10VerbRegexPattern: base 10
	base10VerbRegexPattern verbRegexPattern = `(\d+)`
	// unicodeVerbRegexPattern: Unicode format: U+1234; same as "U+%04X"
	unicodeVerbRegexPattern verbRegexPattern = `(U\+[0-9]+)`
	// scientificNotationLowerVerbRegexPattern: scientific notation, e.g. -1.234456e+78
	scientificNotationLowerVerbRegexPattern verbRegexPattern = `([+-]?[0-9]+\.[0-9]+e\+[0-9]+)`
	// scientificNotationUpperVerbRegexPattern: scientific notation, e.g. -1.234456E+78
	scientificNotationUpperVerbRegexPattern verbRegexPattern = `([+-]?[0-9]+\.[0-9]+E\+[0-9]+)`
	// floatVerbRegexPattern: decimal point but no exponent, e.g. 123.456
	floatVerbRegexPattern verbRegexPattern = `([+-]?[0-9]+\.[0-9]+)`
	// floatSynonymVerbRegexPattern: synonym for %f
	floatSynonymVerbRegexPattern verbRegexPattern = `([+-]?[0-9]+\.[0-9]+)`
	// floatHexLowerVerbRegexPattern: hexadecimal notation (with decimal power of two exponent), e.g. -0x1.23abcp+20
	floatHexLowerVerbRegexPattern verbRegexPattern = `([+-]?0x[a-f0-9]+\.[0-9]+p\+[a-f0-9]+)`
	// floatHexUpperVerbRegexPattern: upper-case hexadecimal notation, e.g. -0X1.23ABCP+20
	floatHexUpperVerbRegexPattern verbRegexPattern = `([+-]?0x[A-F0-9]+\.[0-9]+P\+[A-F0-9]+)`
)

// verbToRegexMapping is a mapping of verbs used in string interpolation within the fmt package and the regular
// expressions that match them. If a particular verb does not exist in this mapping, then there are two possible reasons
// for this:
//
// • The verb can be converted straight to a regex character set, e.g. d -> (\d+).
//
// • The verb cannot exist within a URL without being percent-sign encoded, e.g. %q would result in the double quotes
// being encoded to URL.
var verbToRegexMapping = map[string]string{
	string(stringVerb):                  string(stringVerbRegexPattern),
	string(boolVerb):                    string(boolVerbRegexPattern),
	string(base2Verb):                   string(base2VerbRegexPattern),
	string(charVerb):                    string(charVerbRegexPattern),
	string(base8Verb):                   string(base8VerbRegexPattern),
	string(base8PrefixVerb):             string(base8PrefixVerbRegexPattern),
	string(unicodeVerb):                 string(unicodeVerbRegexPattern),
	string(scientificNotationLowerVerb): string(scientificNotationLowerVerbRegexPattern),
	string(scientificNotationUpperVerb): string(scientificNotationUpperVerbRegexPattern),
	string(floatVerb):                   string(floatVerbRegexPattern),
	string(floatSynonymVerb):            string(floatSynonymVerbRegexPattern),
	string(floatHexLowerVerb):           string(floatHexLowerVerbRegexPattern),
	string(floatHexUpperVerb):           string(floatHexUpperVerbRegexPattern),
}

// regexParserFunc is the signature for functions that is used in regexParsers.
type regexParserFunc func(s string) (any, error)

// regexParsers is a mapping of regular expression patterns to the function that can parse strings that match those
// patterns.
var regexParsers = map[string]regexParserFunc{
	// the word true or false
	string(boolVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseBool(s)
	},
	// base 2
	string(base2VerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseInt(s, 2, 64)
	},
	// the character represented by the corresponding Unicode code point
	string(charVerbRegexPattern): func(s string) (any, error) {
		return s[0], nil
	},
	// base 8
	string(base8VerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseInt(s, 8, 64)
	},
	// base 8 with 0o prefix
	string(base8PrefixVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseInt(s, 8, 64)
	},
	// base 10
	string(base10VerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseInt(s, 10, 64)
	},
	// Unicode format: U+1234; same as "U+%04X"
	string(unicodeVerbRegexPattern): func(s string) (any, error) {
		return nil, nil
	},
	// scientific notation, e.g. -1.234456e+78
	string(scientificNotationLowerVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseFloat(s, 64)
	},
	// scientific notation, e.g. -1.234456E+78
	string(scientificNotationUpperVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseFloat(s, 64)
	},
	// decimal point but no exponent, e.g. 123.456
	string(floatVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseFloat(s, 64)
	},
	// hexadecimal notation (with decimal power of two exponent), e.g. -0x1.23abcp+20
	string(floatHexLowerVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseFloat(s, 64)
	},
	// upper-case hexadecimal notation, e.g. -0X1.23ABCP+20
	string(floatHexUpperVerbRegexPattern): func(s string) (any, error) {
		return strconv.ParseFloat(s, 64)
	},
}

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
	// SteamGetAppList fetches a JSON object of all the names and IDs of the current apps on Steam.
	SteamGetAppList ScrapeURL = "%s://api.steampowered.com/ISteamApps/GetAppList/v2/"
	// SteamSpyAppDetails is the URL for the app details API from Steamspy. Can fetch the details, in JSON, for the
	// given appid.
	SteamSpyAppDetails ScrapeURL = "%s://steamspy.com/api.php?request=appdetails&appid=%d"
	// ItchIOGamePage is the game page for a given developer and game-title combo.
	ItchIOGamePage ScrapeURL = "%s://%s.itch.io/%s"
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
	case SteamGetAppList:
		return "SteamGetAppList"
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
	return regexp.MustCompile(regexp.MustCompile("%([a-zA-Z])").ReplaceAllStringFunc(protocolString, func(s string) string {
		var ok bool
		charSet := strings.ReplaceAll(s, "%", "")
		if s, ok = verbToRegexMapping[charSet]; !ok {
			s = fmt.Sprintf(`(\%s+)`, charSet)
		}
		return s
	}))
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
	metaPattern := regexp.MustCompile(`(?m)(\([^()]+?\))`)
	groups := pattern.FindStringSubmatch(url)[1:]
	groupPatterns := make([]string, 0)
	for _, groupMatches := range metaPattern.FindAllStringSubmatch(pattern.String(), -1) {
		groupPatterns = append(groupPatterns, groupMatches[1:][0])
	}
	if len(groups) != len(groupPatterns) {
		panic(fmt.Errorf(
			"the number of groups matched by %s doesn't match the number of groups found in the pattern (%d vs %d)",
			pattern.String(), len(groups), len(groupPatterns),
		))
	}
	args = make([]any, len(groups))
	for i, group := range groups {
		groupPattern := groupPatterns[i]
		if parseFunc, ok := regexParsers[groupPattern]; ok {
			var err error
			if args[i], err = parseFunc(group); err != nil {
				panic(errors.Wrapf(err, "could not parse string %q using parser for %q", group, groupPattern))
			}
		} else {
			args[i] = group
		}
	}
	return args
}

// Standardise will first extract the args from the given URL then Fill the referred to ScrapeURL with those args.
func (su ScrapeURL) Standardise(url string) string {
	args := su.ExtractArgs(url)
	return su.Fill(args...)
}

// Request creates a new http.MethodGet http.Request for the given ScrapeURL with the given arguments.
func (su ScrapeURL) Request(args ...any) (url string, req *http.Request, err error) {
	url = su.Fill(args...)
	if req, err = http.NewRequest(http.MethodGet, url, nil); err != nil {
		err = errors.Wrapf(err, "request for %q could not be created", url)
	}
	return
}

// Soup fetches the ScrapeURL using the default HTTP client, then parses the returned HTML page into a soup.Root. It
// also returns the http.Response object returned by the http.Get request. A http.Request can be provided, but if nil is
// provided then a default http.MethodGet http.Request will be constructed instead.
func (su ScrapeURL) Soup(req *http.Request, args ...any) (doc *soup.Root, resp *http.Response, err error) {
	if req == nil {
		if _, req, err = su.Request(args...); err != nil {
			return
		}
	}

	if resp, err = http.DefaultClient.Do(req); err != nil {
		err = errors.Wrapf(err, "could not get Steam page %s", req.URL.String())
		return
	}

	if resp.Body != nil {
		defer func(body io.ReadCloser) {
			err = myErrors.MergeErrors(err, errors.Wrapf(body.Close(), "could not close response body to %s", req.URL.String()))
		}(resp.Body)
	}

	var body []byte
	if body, err = io.ReadAll(resp.Body); err != nil {
		err = errors.Wrapf(err, "could not read response body to %s", req.URL.String())
		return
	}

	root := soup.HTMLParse(string(body))
	doc = &root
	return
}

// RetrySoup will run Soup with the given args and try the given function. If the function returns an error then the
// function will be retried up to a total of the given number of maxTries. If minDelay is given, and is not 0, then
// before the function is retried it will sleep for (maxTries + 1 - currentTries) * minDelay. If a non-nil http.Request
// is provided then it will be used to fetch the page for the Soup, otherwise a default http.MethodGet http.Request will
// be constructed instead.
func (su ScrapeURL) RetrySoup(req *http.Request, maxTries int, minDelay time.Duration, try func(doc *soup.Root, resp *http.Response) error, args ...any) error {
	return myErrors.Retry(maxTries, minDelay, func(currentTry int, maxTries int, minDelay time.Duration, args ...any) (err error) {
		log.INFO.Printf("RetrySoup(%s, %v) is on try %d/%d", su.Name(), args, currentTry, maxTries)
		var (
			doc  *soup.Root
			resp *http.Response
		)
		if doc, resp, err = su.Soup(req, args...); err != nil {
			return errors.Wrapf(err, "ran out of tries (%d total) whilst requesting Soup for %s", maxTries, su.String())
		}
		if err = try(doc, resp); err != nil {
			return errors.Wrapf(err, "ran out of tries (%d total) whilst calling try function for %s", maxTries, su.String())
		}
		return nil
	}, args...)
}

// JSON makes a request to the ScrapeURL and parses the response to JSON. As well as returning the parsed JSON as a map,
// it also returns the response to the original HTTP request made to the given ScrapeURL. If a non-nil http.Request is
// provided then it will be used to fetch the JSON resource, otherwise default http.MethodGet http.Request will be
// constructed instead.
func (su ScrapeURL) JSON(req *http.Request, args ...any) (jsonBody map[string]any, resp *http.Response, err error) {
	client := http.Client{Timeout: time.Second * 10}
	if req == nil {
		if _, req, err = su.Request(args...); err != nil {
			return
		}
	}

	if resp, err = client.Do(req); err != nil {
		err = errors.Wrapf(err, "JSON could not be fetched from \"%s\"", req.URL.String())
		return
	}

	if resp.Body != nil {
		defer func(Body io.ReadCloser) {
			err = myErrors.MergeErrors(err, errors.Wrapf(
				Body.Close(),
				"request body for JSON fetched from \"%s\" could not be closed",
				req.URL.String(),
			))
		}(resp.Body)
	}

	var body []byte
	if body, err = io.ReadAll(resp.Body); err != nil {
		err = errors.Wrapf(err, "JSON request body from \"%s\" could not be read", req.URL.String())
		return
	}

	jsonBody = make(map[string]any)
	if err = json.Unmarshal(body, &jsonBody); err != nil {
		err = errors.Wrapf(err, "JSON could not be parsed from response from \"%s\"", req.URL.String())
		return
	}
	return
}

// RetryJSON will run JSON with the given args and try the given function. If the function returns an error then the
// function will be retried up to a total of the given number of maxTries. If minDelay is given, and is not 0, then
// before the function is retried it will sleep for (maxTries + 1 - currentTries) * minDelay. If a non-nil http.Request
// is provided then it will be used to fetch the JSON resource, otherwise default http.MethodGet http.Request will be
// constructed instead.
func (su ScrapeURL) RetryJSON(req *http.Request, maxTries int, minDelay time.Duration, try func(jsonBody map[string]any, resp *http.Response) error, args ...any) error {
	return myErrors.Retry(maxTries, minDelay, func(currentTry int, maxTries int, minDelay time.Duration, args ...any) (err error) {
		log.INFO.Printf("RetryJSON(%s, %v) is on try %d/%d", su.Name(), args, currentTry, maxTries)
		var (
			jsonBody map[string]any
			resp     *http.Response
		)
		if jsonBody, resp, err = su.JSON(req, args...); err != nil {
			return errors.Wrapf(err, "ran out of tries (%d total) whilst requesting JSON for %s", maxTries, su.String())
		}
		if err = try(jsonBody, resp); err != nil {
			return errors.Wrapf(err, "ran out of tries (%d total) whilst calling try function for %s", maxTries, su.String())
		}
		return nil
	}, args...)
}
