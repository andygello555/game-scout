package twitter

import (
	"fmt"
	"time"
)

func ExampleGetRateLimitFromDuration() {
	fmt.Println(GetRateLimitFromDuration(500 * time.Millisecond))
	fmt.Println(GetRateLimitFromDuration(1100 * time.Millisecond))
	fmt.Println(GetRateLimitFromDuration(60 * time.Minute))
	fmt.Println(GetRateLimitFromDuration(61 * time.Minute))
	fmt.Println(GetRateLimitFromDuration(168 * time.Hour))
	fmt.Println(GetRateLimitFromDuration(169 * time.Hour))
	// Output:
	// 200
	// 1000
	// 2200
	// 55000
	// 385000
	// 1650000
}
