package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	"github.com/gorilla/mux"
	"github.com/spf13/cast"
)

// -----------------------------------------------------------------------------
// Pracujeme v souřadnicovém systému WGS-84
// Abychom dokázali přepočítat stupně zeměpisné šířky a délky na pixely,
// musíme znát souřadnice levého horního a pravého dolního okraje radarového snímku ČHMÚ

const (
	lon0 = 11.2673442
	lat0 = 52.1670717
	lon1 = 20.7703153
	lat1 = 48.1
)

type City struct {
	ID   int
	Name string
	Lat  float64
	Lon  float64
	R    uint8
	G    uint8
	B    uint8
}

type Handler struct {
	m              sync.RWMutex
	Cities         []*City
	CitiesWithRain []*City
}

func downloadRadar(dateTxt string) []byte {
	url := fmt.Sprintf("https://www.chmi.cz/files/portal/docs/meteo/rad/inca-cz/data/czrad-z_max3d/pacz2gmaps3.z_max3d.%s.0.png", dateTxt)
	log.Printf("Downloading file: %s", url)
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		log.Printf("HTTP %d: Cannot download file", resp.StatusCode)
		return nil
	}

	log.Printf("Succesfully downloaded")
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body
}

func rgbText(r, g, b uint8, text string) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", r, g, b, text)
}

func getAvgColor(bitmap *image.NRGBA, x, y int) (uint8, uint8, uint8) {
	var totalR, totalG, totalB, total uint32

	for xx := -4; xx <= 4; xx++ {
		for yy := -4; yy <= 4; yy++ {
			r, g, b, _ := bitmap.At(x+xx, y+yy).RGBA()
			totalR += r / 257
			totalG += g / 257
			totalB += b / 257
			total++
		}
	}

	return uint8(totalR / total), uint8(totalG / total), uint8(totalB / total)
}

func (h *Handler) LoadCities() {
	file, err := os.Open("mesta.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = ';'
	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}

	for _, record := range records {
		city := &City{
			ID:   cast.ToInt(record[0]),
			Name: record[1],
			Lat:  cast.ToFloat64(record[2]),
			Lon:  cast.ToFloat64(record[3]),
		}

		h.Cities = append(h.Cities, city)
	}
}

func (h *Handler) BackgroundLoop() {
	for {
		log.Println("Starting background loop")
		func() {
			// delete old radar_a_mesta_*.png files
			files, err := os.ReadDir(".")
			if err != nil {
				log.Println(err)
			}
			for _, file := range files {
				if !strings.HasPrefix(file.Name(), "radar_a_mesta_") {
					continue
				}

				// if file is older than 1 hour, delete it
				fileInfo, err := file.Info()
				if err != nil {
					log.Println(err)
					continue
				}

				if time.Since(fileInfo.ModTime()) < time.Hour {
					continue
				}

				log.Printf("Deleting old file %s", file.Name())

				err = os.Remove(file.Name())
				if err != nil {
					log.Println(err)
				}
			}

			date := time.Now().UTC()

			format := "20060102.1504"
			formattedDate := date.Format(format)
			dateTxt := formattedDate[:len(format)-1] + "0"

			p := fmt.Sprintf("radar_a_mesta_%s.png", dateTxt)
			if _, err := os.Stat(p); err == nil {
				log.Println("Already exists")
				return
			}

			content := downloadRadar(dateTxt)
			if content == nil {
				log.Println("Cannot download radar data, skipping")
				return
			}

			img, err := imaging.Decode(bytes.NewReader(content))
			if err != nil {
				log.Fatal(err)
			}
			bitmap := imaging.Clone(img)

			lonPixelSize := (lon1 - lon0) / float64(bitmap.Bounds().Dx())
			latPixelSize := (lat0 - lat1) / float64(bitmap.Bounds().Dy())

			h.m.Lock()
			defer h.m.Unlock()
			h.CitiesWithRain = []*City{}

			for _, city := range h.Cities {
				x := int((city.Lon - lon0) / lonPixelSize)
				y := int((lat0 - city.Lat) / latPixelSize)
				r, g, b := getAvgColor(bitmap, x, y)

				if r+g+b > 0 {
					draw.Draw(bitmap, image.Rect(x-5, y-5, x+5, y+5), &image.Uniform{color.RGBA{r, g, b, 255}}, image.Point{}, draw.Src)
					log.Printf("💦  It's raining in %s (%d) %s  R=%d G=%d B=%d", city.Name, city.ID, rgbText(r, g, b, "■"), r, g, b)
					city.R = r
					city.G = g
					city.B = b
					h.CitiesWithRain = append(h.CitiesWithRain, city)
				} else {
					draw.Draw(bitmap, image.Rect(x-5, y-5, x+5, y+5), &image.Uniform{color.RGBA{0, 0, 0, 255}}, image.Point{}, draw.Src)
				}
			}

			if len(h.CitiesWithRain) == 0 {
				log.Println("It looks like it's not raining!")
			}

			file, err := os.Create(fmt.Sprintf("radar_a_mesta_%s.png", dateTxt))
			if err != nil {
				log.Fatal(err)
			}
			defer file.Close()

			err = imaging.Encode(file, bitmap, imaging.PNG)
			if err != nil {
				log.Fatal(err)
			}
		}()

		time.Sleep(60 * time.Second)
	}
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	h.m.RLock()
	defer h.m.RUnlock()
	json.NewEncoder(w).Encode(h.CitiesWithRain)
}

func main() {
	log.SetOutput(os.Stdout)

	handler := &Handler{}
	handler.LoadCities()

	go handler.BackgroundLoop()

	r := mux.NewRouter()
	r.HandleFunc("/", handler.HandleGet).Methods("GET")

	log.Fatal(http.ListenAndServe(":8080", r))
}
