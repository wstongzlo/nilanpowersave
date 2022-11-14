package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/brutella/hc"
	"github.com/brutella/hc/accessory"
	"github.com/brutella/hc/characteristic"
	"github.com/brutella/hc/service"
	"github.com/gocolly/colly"
	"github.com/pjuzeliunas/nilan"
	"github.com/theherk/viper"
)

// Nilan CTS700 accessory
type Nilan struct {
	*accessory.Accessory

	CentralHeatingSwitch  *service.Switch
	VentilationThermostat *NilanFanThermostat
	OutdoorTemp           *service.TemperatureSensor
	Fan                   *NilanFan
	HotWaterSwitch        *service.Switch
	HotWater              *service.Thermostat
	SupplyFlow            *service.Thermostat
	//for save power mode setting
	AutoPowerSaveModeSwitch       *service.Switch
	MustHeatTemperatureDifference *service.Thermostat
	StopHeatTemperatureDifference *service.Thermostat
	RunHours                      *service.Thermostat
}

// NilanFanThermostat service
type NilanFanThermostat struct {
	*service.Thermostat
	CurrentRelativeHumidity *characteristic.CurrentRelativeHumidity
}

// NewNilanFanThermostat instantiates Nilan Central Heating service
func NewNilanFanThermostat() *NilanFanThermostat {
	svc := NilanFanThermostat{}
	svc.Thermostat = service.NewThermostat()

	svc.CurrentRelativeHumidity = characteristic.NewCurrentRelativeHumidity()
	svc.AddCharacteristic(svc.CurrentRelativeHumidity.Characteristic)

	return &svc
}

// NilanFan service
type NilanFan struct {
	*service.FanV2
	RotationSpeed *characteristic.RotationSpeed
}

// NewNilanFan instantiates Nilan Fan service
func NewNilanFan() *NilanFan {
	svc := NilanFan{}
	svc.FanV2 = service.NewFanV2()

	svc.RotationSpeed = characteristic.NewRotationSpeed()
	svc.RotationSpeed.SetMinValue(25)
	svc.RotationSpeed.SetMaxValue(100)
	svc.RotationSpeed.SetStepValue(25)
	svc.AddCharacteristic(svc.RotationSpeed.Characteristic)

	return &svc
}

var (
	isAutoSavePowerMode           bool
	runHours                      int
	mustHeatTemperatureDifference int
	stopHeatTemperatureDifference int
)

// NewNilan sets Nilan accessory instance up
func NewNilan(info accessory.Info) *Nilan {
	acc := Nilan{}
	acc.Accessory = accessory.New(info, accessory.TypeHeater)

	//start auto save power mode components
	acc.AutoPowerSaveModeSwitch = service.NewSwitch()
	acc.AutoPowerSaveModeSwitch.AddCharacteristic(newName("Auto SaveMode"))
	acc.AutoPowerSaveModeSwitch.On.OnValueRemoteUpdate(func(on bool) {
		log.Printf("Auto save mode active: %v\n", on)
		isAutoSavePowerMode = on
		viper.Set("savemode.on", on)
		viper.WriteConfig()
	})

	acc.MustHeatTemperatureDifference = service.NewThermostat()
	acc.MustHeatTemperatureDifference.AddCharacteristic(newName("Must heat temperature difference"))
	acc.MustHeatTemperatureDifference.TargetTemperature.SetMinValue(1.0)
	acc.MustHeatTemperatureDifference.TargetTemperature.SetMaxValue(50.0)
	acc.MustHeatTemperatureDifference.TargetTemperature.SetStepValue(1.0)
	acc.MustHeatTemperatureDifference.TargetTemperature.SetValue(float64(mustHeatTemperatureDifference))
	acc.MustHeatTemperatureDifference.TargetTemperature.OnValueRemoteUpdate(func(tFloat float64) {
		log.Printf("Setting new must heat target temperature: %v\n", tFloat)
		mustHeatTemperatureDifference = int(tFloat)
		viper.Set("setting.mustheatdf", mustHeatTemperatureDifference)
		viper.WriteConfig()
		t := int(tFloat * 10.0)
		if !(t >= 10 && t <= 500) {
			log.Println("Invalid must heat target temperature setting. Ignoring change request.")
			return
		}
	})

	acc.StopHeatTemperatureDifference = service.NewThermostat()
	acc.StopHeatTemperatureDifference.AddCharacteristic(newName("Must stop heat temperature difference"))
	acc.StopHeatTemperatureDifference.TargetTemperature.SetMinValue(1.0)
	acc.StopHeatTemperatureDifference.TargetTemperature.SetMaxValue(50.0)
	acc.StopHeatTemperatureDifference.TargetTemperature.SetStepValue(1.0)
	acc.StopHeatTemperatureDifference.TargetTemperature.SetValue(float64(stopHeatTemperatureDifference))
	acc.StopHeatTemperatureDifference.TargetTemperature.OnValueRemoteUpdate(func(tFloat float64) {
		log.Printf("Setting new stop heat target temperature: %v\n", tFloat)
		stopHeatTemperatureDifference = int(tFloat)
		viper.Set("setting.stopheatdf", stopHeatTemperatureDifference)
		viper.WriteConfig()
		t := int(tFloat * 10.0)
		if !(t >= 10 && t <= 500) {
			log.Println("Invalid stop heat target temperature setting. Ignoring change request.")
			return
		}
	})

	acc.RunHours = service.NewThermostat()
	acc.RunHours.AddCharacteristic(newName("Run hours"))
	acc.RunHours.TargetTemperature.SetMinValue(1.0)
	acc.RunHours.TargetTemperature.SetMaxValue(23.0)
	acc.RunHours.TargetTemperature.SetStepValue(1.0)
	acc.RunHours.TargetTemperature.SetValue(float64(runHours))
	acc.RunHours.TemperatureDisplayUnits.Description = "hours"
	acc.RunHours.TargetTemperature.OnValueRemoteUpdate(func(tFloat float64) {
		log.Printf("Setting new run hours: %v\n", tFloat)
		runHours = int(tFloat)
		viper.Set("setting.runhours", runHours)
		viper.WriteConfig()
		t := int(tFloat * 10.0)
		if !(t >= 10 && t <= 230) {
			log.Println("Invalid run hours setting. Ignoring change request.")
			return
		}
	})
	//end auto save power mode components

	acc.CentralHeatingSwitch = service.NewSwitch()
	acc.CentralHeatingSwitch.AddCharacteristic(newName("Central Heating"))
	acc.CentralHeatingSwitch.On.OnValueRemoteUpdate(func(on bool) {
		log.Printf("Setting Central Heating active: %v\n", on)

		s := nilan.Settings{}
		p := !on
		s.CentralHeatingPaused = &p
		if !on {
			s.CentralHeatingPauseDuration = new(int)
			*s.CentralHeatingPauseDuration = 180
		}

		c := nilanController()
		c.SendSettings(s)
	})

	acc.VentilationThermostat = NewNilanFanThermostat()
	acc.VentilationThermostat.Primary = true
	acc.VentilationThermostat.AddCharacteristic(newName("Room Temperature"))
	acc.VentilationThermostat.TargetHeatingCoolingState.OnValueRemoteUpdate(func(state int) {
		c := nilanController()
		switch state {
		case characteristic.TargetHeatingCoolingStateOff:
			p := true
			s := nilan.Settings{VentilationOnPause: &p}
			c.SendSettings(s)
		case characteristic.TargetHeatingCoolingStateHeat:
			p := false
			m := 2 // heating
			s := nilan.Settings{VentilationMode: &m, VentilationOnPause: &p}
			c.SendSettings(s)
		case characteristic.TargetHeatingCoolingStateCool:
			p := false
			m := 1 // cooling
			s := nilan.Settings{VentilationMode: &m, VentilationOnPause: &p}
			c.SendSettings(s)
		case characteristic.TargetHeatingCoolingStateAuto:
			p := false
			m := 0 // auto
			s := nilan.Settings{VentilationMode: &m, VentilationOnPause: &p}
			c.SendSettings(s)
		}
	})
	acc.VentilationThermostat.TemperatureDisplayUnits.SetValue(characteristic.TemperatureDisplayUnitsCelsius)
	acc.VentilationThermostat.TargetTemperature.SetMinValue(5.0)
	acc.VentilationThermostat.TargetTemperature.SetMaxValue(40.0)
	acc.VentilationThermostat.TargetTemperature.SetStepValue(1.0)
	acc.VentilationThermostat.TargetTemperature.OnValueRemoteUpdate(func(tFloat float64) {
		log.Printf("Setting new Room target temperature: %v\n", tFloat)
		t := int(tFloat * 10.0)
		if !(t >= 50 && t <= 400) {
			log.Println("Invalid Room temperature setting. Ignoring change request.")
			return
		}
		s := nilan.Settings{DesiredRoomTemperature: &t}
		c := nilanController()
		c.SendSettings(s)
	})

	acc.Fan = NewNilanFan()
	acc.Fan.AddCharacteristic(newName("Fan"))
	acc.Fan.Active.Perms = []string{characteristic.PermRead, characteristic.PermEvents}
	acc.Fan.RotationSpeed.OnValueRemoteUpdate(func(newSpeed float64) {
		log.Printf("Setting new Fan speed: %v\n", newSpeed)
		speed := nilan.FanSpeed(100 + int(newSpeed)/25)
		if !(speed >= 101 && speed <= 104) {
			log.Println("Invalid Fan speed. Ignoring change request.")
			return
		}
		s := nilan.Settings{FanSpeed: &speed}
		c := nilanController()
		c.SendSettings(s)
	})

	acc.HotWaterSwitch = service.NewSwitch()
	acc.HotWaterSwitch.AddCharacteristic(newName("Hot Water Production"))
	acc.HotWaterSwitch.On.OnValueRemoteUpdate(func(on bool) {
		log.Printf("Setting DHW active: %v\n", on)

		s := nilan.Settings{}
		p := !on
		s.DHWProductionPaused = &p

		if !on {
			s.DHWProductionPauseDuration = new(int)
			*s.DHWProductionPauseDuration = 180
		}

		c := nilanController()
		c.SendSettings(s)
	})

	acc.HotWater = service.NewThermostat()
	acc.HotWater.AddCharacteristic(newName("Hot Water"))
	acc.HotWater.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateHeat)
	acc.HotWater.TargetHeatingCoolingState.Perms = []string{characteristic.PermRead, characteristic.PermEvents}
	acc.HotWater.TemperatureDisplayUnits.SetValue(characteristic.TemperatureDisplayUnitsCelsius)
	acc.HotWater.TargetTemperature.SetMinValue(10.0)
	acc.HotWater.TargetTemperature.SetMaxValue(60.0)
	acc.HotWater.TargetTemperature.SetStepValue(1.0)
	acc.HotWater.TargetTemperature.OnValueRemoteUpdate(func(tFloat float64) {
		log.Printf("Setting new DHW target temperature: %v\n", tFloat)
		t := int(tFloat * 10.0)
		if !(t >= 100 && t <= 600) {
			log.Println("Invalid DHW temperature setting. Ignoring change request.")
			return
		}
		s := nilan.Settings{DesiredDHWTemperature: &t}
		c := nilanController()
		c.SendSettings(s)
	})

	acc.SupplyFlow = service.NewThermostat()
	acc.SupplyFlow.AddCharacteristic(newName("Central Heating"))
	acc.SupplyFlow.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateHeat)
	acc.SupplyFlow.TargetHeatingCoolingState.Perms = []string{characteristic.PermRead, characteristic.PermEvents}
	acc.SupplyFlow.TemperatureDisplayUnits.SetValue(characteristic.TemperatureDisplayUnitsCelsius)
	acc.SupplyFlow.TargetTemperature.SetMinValue(5.0)
	acc.SupplyFlow.TargetTemperature.SetMaxValue(50.0)
	acc.SupplyFlow.TargetTemperature.SetStepValue(1.0)
	acc.SupplyFlow.TargetTemperature.OnValueRemoteUpdate(func(tFloat float64) {
		log.Printf("Setting new Supply Flow target temperature: %v\n", tFloat)
		t := int(tFloat * 10.0)
		if !(t >= 50 && t <= 500) {
			log.Println("Invalid Supply Flow temperature setting. Ignoring change request.")
			return
		}
		s := nilan.Settings{SetpointSupplyTemperature: &t}
		c := nilanController()
		c.SendSettings(s)
	})

	acc.OutdoorTemp = service.NewTemperatureSensor()
	acc.OutdoorTemp.AddCharacteristic(newName("Outdoor Temperature"))
	acc.OutdoorTemp.CurrentTemperature.SetMinValue(-40)
	acc.OutdoorTemp.CurrentTemperature.SetMaxValue(160)

	acc.AddService(acc.CentralHeatingSwitch.Service)
	acc.AddService(acc.VentilationThermostat.Service)
	acc.AddService(acc.OutdoorTemp.Service)
	acc.AddService(acc.Fan.Service)
	acc.AddService(acc.HotWaterSwitch.Service)
	acc.AddService(acc.HotWater.Service)
	acc.AddService(acc.SupplyFlow.Service)
	acc.AddService(acc.AutoPowerSaveModeSwitch.Service)
	acc.AddService(acc.MustHeatTemperatureDifference.Service)
	acc.AddService(acc.StopHeatTemperatureDifference.Service)
	acc.AddService(acc.RunHours.Service)
	return &acc
}

func newName(n string) *characteristic.Characteristic {
	char := characteristic.NewName()
	char.String.SetValue(n)
	return char.Characteristic
}

func nilanController() nilan.Controller {
	conf := nilan.CurrentConfig()
	return nilan.Controller{Config: conf}
}

func updateReadings(acc *Nilan) {
	c := nilanController()
	r, _ := c.FetchReadings()
	s, _ := c.FetchSettings()

	if *s.CentralHeatingIsOn && !*s.CentralHeatingPaused {
		acc.CentralHeatingSwitch.On.SetValue(true)
		acc.SupplyFlow.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateHeat)
	} else {
		acc.CentralHeatingSwitch.On.SetValue(false)
		acc.SupplyFlow.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateOff)
	}

	acc.VentilationThermostat.CurrentTemperature.SetValue(float64(r.RoomTemperature) / 10.0)
	acc.VentilationThermostat.TargetTemperature.SetValue(float64(*s.DesiredRoomTemperature) / 10.0)
	acc.VentilationThermostat.CurrentRelativeHumidity.SetValue(float64(r.ActualHumidity))

	if *s.VentilationOnPause {
		acc.VentilationThermostat.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateOff)
		acc.VentilationThermostat.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateOff)
	} else {
		switch *s.VentilationMode {
		case 0: // auto
			acc.VentilationThermostat.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateAuto)
			acc.VentilationThermostat.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateOff)
		case 1: // cooling
			acc.VentilationThermostat.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateCool)
			acc.VentilationThermostat.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateCool)
		case 2: // heating
			acc.VentilationThermostat.TargetHeatingCoolingState.SetValue(characteristic.TargetHeatingCoolingStateHeat)
			acc.VentilationThermostat.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateHeat)
		}
	}

	if *s.VentilationOnPause {
		acc.Fan.Active.SetValue(characteristic.ActiveInactive)
	} else {
		acc.Fan.Active.SetValue(characteristic.ActiveActive)
	}
	acc.Fan.RotationSpeed.SetValue((float64(*s.FanSpeed) - 100) * 25.0)

	acc.HotWater.CurrentTemperature.SetValue(float64(r.DHWTankTopTemperature) / 10.0)
	acc.HotWater.TargetTemperature.SetValue(float64(*s.DesiredDHWTemperature) / 10.0)
	if *s.DHWProductionPaused {
		acc.HotWater.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateOff)
		acc.HotWaterSwitch.On.SetValue(false)
	} else {
		acc.HotWater.CurrentHeatingCoolingState.SetValue(characteristic.CurrentHeatingCoolingStateHeat)
		acc.HotWaterSwitch.On.SetValue(true)
	}

	acc.SupplyFlow.CurrentTemperature.SetValue(float64(r.SupplyFlowTemperature) / 10.0)
	acc.SupplyFlow.TargetTemperature.SetValue(float64(*s.SetpointSupplyTemperature) / 10.0)

	acc.OutdoorTemp.CurrentTemperature.SetValue(float64(r.OutdoorTemperature) / 10.0)
}

func startUpdatingReadings(ac *Nilan, freq time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			// In case of failure: waiting and trying again
			log.Printf("Sync with Nilan did fail: %v\n", r)
			time.Sleep(freq)
			startUpdatingReadings(ac, freq)
		}
	}()
	for {
		updateReadings(ac)
		time.Sleep(freq) // 5 sec delay
	}
}

// Configure when to start the heating
func autoConfigure(freq time.Duration) {

	c := nilanController()

	var runOnce, initialOnce bool
	runOnce = true
	initialOnce = true

	lowestThreePrices := make([]float64, runHours)
	lowestThreeHours := make([]int, runHours)

	for {
		dt := time.Now()

		log.Printf("get lowest price once is %t", runOnce)
		// Get lowest electric price from andel energi
		if (dt.Local().Hour() == 20 && runOnce) || initialOnce {
			scrapUrl := "https://andelenergi.dk/kundeservice/aftaler-og-priser/timepris/"
			lowestThreeHours, lowestThreePrices, _ = GetLowestPriceHours(scrapUrl, runHours)
			runOnce = false
			initialOnce = false
		} else if dt.Local().Hour() != 20 {
			runOnce = true
		}
		r, _ := c.FetchReadings()
		s, _ := c.FetchSettings()
		log.Println("The lowest electric price three hours are:")
		for i := 0; i < runHours; i++ {
			log.Printf(" %d", lowestThreeHours[i])
		}
		log.Println("The lowest electric price are:")
		for i := 0; i < runHours; i++ {
			log.Printf(" %g", lowestThreePrices[i])
		}

		//If it's in the hours of heating
		inHoursHeating := false
		for i := 0; i < runHours; i++ {
			if lowestThreeHours[i] == dt.Local().Hour() {
				inHoursHeating = true
			}
		}

		//if dt.Local().Hour() >= 0 && dt.Local().Hour() <= 2 {
		if inHoursHeating || (*s.DesiredDHWTemperature-r.DHWTankTopTemperature)/10 >= mustHeatTemperatureDifference {
			log.Printf("night:hot water temperature settting is %v and actual temperature is %v and production pause is %v", *s.DesiredDHWTemperature, r.DHWTankTopTemperature, *s.DHWProductionPaused)
			if *s.DHWProductionPaused && isAutoSavePowerMode {
				log.Printf("Open the hot water")
				s := nilan.Settings{}
				p := false
				s.DHWProductionPaused = &p
				s.DHWProductionPauseDuration = new(int)
				*s.DHWProductionPauseDuration = 0
				c.SendSettings(s)
			}

		} else {
			log.Printf("day:hot water temperature settting is %v and actual temperature is %v and production pause is %v", *s.DesiredDHWTemperature, r.DHWTankTopTemperature, *s.DHWProductionPaused)
			if (*s.DesiredDHWTemperature-r.DHWTankTopTemperature)/10 < stopHeatTemperatureDifference && !*s.DHWProductionPaused && isAutoSavePowerMode {
				log.Printf("Close the hot water")
				s := nilan.Settings{}
				p := true
				s.DHWProductionPaused = &p
				s.DHWProductionPauseDuration = new(int)
				*s.DHWProductionPauseDuration = 180
				c.SendSettings(s)
			}
		}
		time.Sleep((freq))
	}

}

// Return three lowest price hour
func GetLowestPriceHours(scrapURL string, runHours int) ([]int, []float64, error) {
	//define struct to accept json data
	type DateAndDay struct {
		Date string `json:"date"`
		Day  string `json:"day"`
	}

	type Earea struct {
		Labels []string     `json:"labels"`
		Values []string     `json:"values"`
		Dates  []DateAndDay `json:"dates"`
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

	//Create nilan logfile
	f, err := os.OpenFile("/home/kevin/nilan-log/nilanlogfile"+time.Now().Format("2006-01-02"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	//f, err := os.OpenFile("/home/Emil/nilan-log/nilanlogfile"+time.Now().Format("2006-01-02"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	log.SetOutput(f)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	defer f.Close()
	log.Println("Start the Nilan-hk program!!!")
	//read config.toml to initialize the variable
	viper.SetConfigName("config")               // name of config file (without extension)
	viper.AddConfigPath("/home/kevin/nilan-hk") // optionally look for config in the working directory
	err1 := viper.ReadInConfig()                // Find and read the config file
	if err1 != nil {
		log.Fatalf("error opening config file: %v", err)
	}

	isAutoSavePowerMode = viper.GetBool("savemode.on")
	runHours = viper.GetInt("setting.runhours")
	mustHeatTemperatureDifference = viper.GetInt("setting.mustheatdf")
	stopHeatTemperatureDifference = viper.GetInt("setting.stopheatdf")
	// create an accessory
	info := accessory.Info{Name: "Nilan"}
	ac := NewNilan(info)
	// set auto power save mode to open
	ac.AutoPowerSaveModeSwitch.On.SetValue(isAutoSavePowerMode)

	go startUpdatingReadings(ac, 5*time.Second)

	go autoConfigure(60 * time.Second)

	pin, pinDefined := os.LookupEnv("HK_PIN")
	if !pinDefined {
		log.Panic("HK_PIN environment variable with 8 digit PIN code must be present")
	}
	port, portDefined := os.LookupEnv("HK_PORT")

	// configure the ip transport
	config := hc.Config{Pin: pin}
	if portDefined {
		config.Port = port
	}

	t, err := hc.NewIPTransport(config, ac.Accessory)
	if err != nil {
		log.Panic(err)
	}

	hc.OnTermination(func() {
		<-t.Stop()
	})

	t.Start()

}
