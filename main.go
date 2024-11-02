package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"image/color"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

// Tiingo API Configuration
const apiKey = "YOUR_API_KEY" // Replace with your actual Tiingo API key
const apiURL = "https://api.tiingo.com/tiingo/daily/%s/prices?startDate=%s&token=" + apiKey

// StockData holds API response data
type StockData struct {
	Symbol string  `json:"ticker"`
	Close  float64 `json:"close"`
	Date   string  `json:"date"`
}

// Embed the ARIMA executable from the assets folder
//
//go:embed assets/arima_predict.exe
var arimaPredictExe []byte

// Define the fetch button before main
var fetchButton *widget.Button

// fetchStockData retrieves stock data for a given symbol from Tiingo API
func fetchStockData(symbol string, months int) ([]StockData, error) {
	startDate := time.Now().AddDate(0, -months, 0).Format("2006-01-02")
	url := fmt.Sprintf(apiURL, symbol, startDate)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var stockData []StockData
	if err := json.Unmarshal(body, &stockData); err != nil {
		return nil, err
	}

	return stockData, nil
}

// callPythonARIMA calls the embedded ARIMA executable and returns predictions
func callPythonARIMA(prices []float64) ([]float64, error) {
	data := map[string]interface{}{
		"prices": prices,
	}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	// Create a temporary executable file
	tempExe, err := ioutil.TempFile("", "arima_predict_*.exe")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tempExe.Name()) // Clean up after execution

	// Write the embedded executable to the temporary file
	if _, err := tempExe.Write(arimaPredictExe); err != nil {
		return nil, err
	}
	tempExe.Close() // Close the file so it can be executed

	// Run the temporary executable
	cmd := exec.Command(tempExe.Name())
	cmd.Stdin = bytes.NewReader(jsonData)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		log.Println("Error calling ARIMA prediction:", err, "Stderr:", stderr.String())
		return nil, err
	}

	var predictions []float64
	err = json.Unmarshal(out.Bytes(), &predictions)
	if err != nil {
		return nil, err
	}

	return predictions, nil
}

// plotData creates and saves a graph with stock data and prediction
func plotData(prices []float64, predictions []float64, symbol string) error {
	p := plot.New()
	p.Title.Text = "Stock Prices and Predictions for " + symbol
	p.X.Label.Text = "Days"
	p.Y.Label.Text = "Price"

	startIndex := len(prices) - int(math.Min(90, float64(len(prices))))

	stockPoints := make(plotter.XYs, len(prices)-startIndex)
	for i := startIndex; i < len(prices); i++ {
		stockPoints[i-startIndex].X = float64(i - startIndex)
		stockPoints[i-startIndex].Y = prices[i]
	}

	predPoints := make(plotter.XYs, len(predictions))
	for i := range predictions {
		predPoints[i].X = float64(len(prices) - startIndex + i)
		predPoints[i].Y = predictions[i]
	}

	line, _ := plotter.NewLine(stockPoints)
	line.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255}

	predLine, _ := plotter.NewLine(predPoints)
	predLine.Color = color.RGBA{G: 255, A: 255}

	p.Add(line, predLine)
	p.Legend.Add("Stock", line)
	p.Legend.Add("Prediction", predLine)

	return p.Save(8*vg.Inch, 4*vg.Inch, "plot.png")
}

func main() {
	myApp := app.New()
	myWindow := myApp.NewWindow("Stock Analyzer")
	myWindow.Resize(fyne.NewSize(800, 600))

	stockEntry := widget.NewEntry()
	stockEntry.SetPlaceHolder("Enter Stock Symbol (e.g., AAPL)")

	img := canvas.NewImageFromFile("plot.png")
	img.FillMode = canvas.ImageFillOriginal

	// Initialize fetchButton
	fetchButton = widget.NewButton("Fetch Data", func() {
		symbol := stockEntry.Text
		data, err := fetchStockData(symbol, 12) // Fetch data for the last 12 months
		if err != nil {
			log.Println("Error fetching data:", err)
			return
		}

		log.Printf("Fetched %d data points for symbol: %s\n", len(data), symbol)

		if len(data) == 0 {
			log.Println("No data returned for symbol:", symbol)
			return
		}

		prices := make([]float64, len(data))
		for i, d := range data {
			prices[i] = d.Close
		}

		log.Printf("Prices for %s: %v\n", symbol, prices)

		if len(prices) < 2 { // Ensure enough data for predictions
			log.Println("Not enough data points for predictions.")
			return
		}

		predictions, err := callPythonARIMA(prices)
		if err != nil {
			log.Println("Error calling ARIMA prediction:", err)
			return
		}

		if err := plotData(prices, predictions, symbol); err != nil {
			log.Println("Error plotting data:", err)
			return
		}

		// Update the image
		img = canvas.NewImageFromFile("plot.png")
		img.FillMode = canvas.ImageFillOriginal
		myWindow.SetContent(container.NewVBox(stockEntry, fetchButton, img))
	})

	myWindow.SetContent(container.NewVBox(stockEntry, fetchButton, img))
	myWindow.ShowAndRun()
}
