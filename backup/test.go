package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gocolly/colly"
)

// Return three lowest price hour
func GetLowestPriceHours(scrapURL string, runHours int) ([]int, []float64, error) {
	//define struct to accept json data
	type DateAndDay struct {
		Date string `json:"date"`
		Day  string `json:"day"`
	}

	type Earea struct {
		Labels             []string     `json:"labels"`
		Values             []string     `json:"values"`
		ValuesDistribution []string     `json:"valuesDistribution"` //New added for transport expense 20231113
		Dates              []DateAndDay `json:"dates"`
	}

	type Eall struct {
		East Earea `json:"east"`
		West Earea `json:"west"`
	}

	c := colly.NewCollector()
	minvalue := make([]float64, runHours)
	minhour := make([]int, runHours)
	for i := 0; i < runHours; i++ {
		minvalue[i] = 9999
		minhour[i] = -1
	}
	c.OnHTML("div#chart-component", func(e *colly.HTMLElement) {

		priceJson := e.Attr("data-chart")
		var str Eall
		err := json.Unmarshal([]byte(priceJson), &str)
		if err != nil {
			return
		}

		if strings.TrimSpace(str.East.Dates[len(str.East.Dates)-1].Day) == strconv.Itoa(time.Now().Day()) {
			for i := 0; i < runHours; i++ {
				for j := 0; j < 24; j++ {
					hasCompared := false
					for k := 0; k <= i; k++ {
						if j >= 0 && j < 4 {
							if j+20 == minhour[k] {
								hasCompared = true
							}
						} else {
							if j-4 == minhour[k] {
								hasCompared = true
							}
						}

					}
					if !hasCompared {
						s1, _ := strconv.ParseFloat(str.East.Values[len(str.East.Values)-28+j], 64)
						ete, _ := strconv.ParseFloat(str.East.ValuesDistribution[len(str.East.ValuesDistribution)-28+j], 64) // add transport expense

						s1 = s1 + ete
						if s1 < minvalue[i] {
							if j >= 0 && j < 4 {
								minvalue[i] = s1
								minhour[i] = j + 20
							} else {
								minvalue[i] = s1
								minhour[i] = j - 4
							}
						}
					}

				}

			}
		} else if strings.TrimSpace(str.East.Dates[len(str.East.Dates)-1].Day) == strconv.Itoa(time.Now().Day()+1) {
			if time.Now().Local().Hour() < 20 {
				for i := 0; i < runHours; i++ {
					for j := 0; j < 24; j++ {
						hasCompared := false
						for k := 0; k <= i; k++ {
							if j >= 0 && j < 4 {
								if j+20 == minhour[k] {
									hasCompared = true
								}
							} else {
								if j-4 == minhour[k] {
									hasCompared = true
								}
							}
						}
						if !hasCompared {
							s1, _ := strconv.ParseFloat(str.East.Values[len(str.East.Values)-52+j], 64)
							ete, _ := strconv.ParseFloat(str.East.ValuesDistribution[len(str.East.ValuesDistribution)-52+j], 64) // add transport expense
							s1 = s1 + ete
							if s1 < minvalue[i] {
								if j >= 0 && j < 4 {
									minvalue[i] = s1
									minhour[i] = j + 20
								} else {
									minvalue[i] = s1
									minhour[i] = j - 4
								}

							}
						}

					}

				}
			} else {
				for i := 0; i < runHours; i++ {
					for j := 0; j < 24; j++ {
						hasCompared := false

						if j <= 23 && j >= 4 {
							for k := 0; k <= i; k++ {
								if j-4 == minhour[k] {
									hasCompared = true
								}
							}
							if !hasCompared {
								s1, _ := strconv.ParseFloat(str.East.Values[len(str.East.Values)-28+j], 64)
								ete, _ := strconv.ParseFloat(str.East.ValuesDistribution[len(str.East.ValuesDistribution)-28+j], 64) // add transport expense
								s1 = s1 + ete
								if s1 < minvalue[i] {
									minvalue[i] = s1
									minhour[i] = j - 4
								}
							}
						} else {
							for k := 0; k <= i; k++ {
								if j+20 == minhour[k] {
									hasCompared = true
								}
							}
							if !hasCompared {
								s1, _ := strconv.ParseFloat(str.East.Values[len(str.East.Values)-28+j], 64)
								ete, _ := strconv.ParseFloat(str.East.ValuesDistribution[len(str.East.ValuesDistribution)-28+j], 64) // add transport expense
								s1 = s1 + ete
								if s1 < minvalue[i] {
									minvalue[i] = s1
									minhour[i] = j + 20
								}
							}
						}
					}
				}
			}

		}

	})

	c.OnRequest(func(r *colly.Request) {
		fmt.Printf("Visiting %s\n", r.URL)
	})
	c.OnError(func(r *colly.Response, e error) {
		fmt.Printf("Error while scraping:%s", e.Error())
	})

	c.Visit(scrapURL)

	return minhour, minvalue, nil
}

func main() {
	scrapUrl := "https://andelenergi.dk/kundeservice/aftaler-og-priser/timepris/"
	lowestThreeHours, lowestThreePrices, _ := GetLowestPriceHours(scrapUrl, 5)
	fmt.Println(lowestThreeHours)
	fmt.Println(lowestThreePrices)
}
