package main

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rbxb/httpfilter"
	"github.com/rbxb/place"
)

// Struct to parse Supabase login response
type AuthResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	User        struct {
		ID string `json:"id"`
	} `json:"user"`
}

var port string
var root string
var loadPath string
var savePath string
var logPath string
var width int
var height int
var count int
var saveInterval int
var enableWL bool
var whitelistPath string
var loadRecordPath string
var saveRecordPath string
var bucketUrl string
var apiUrl string
var userEmail string
var userPassword string
var anonKey string

func init() {
	flag.StringVar(&port, "port", ":8080", "The address and port the fileserver listens at.")
	flag.StringVar(&root, "root", "./root", "The directory serving files.")
	flag.StringVar(&loadPath, "load", "", "The png to load as the canvas.")
	flag.StringVar(&savePath, "save", "./place.png", "The path to save the canvas.")
	flag.StringVar(&logPath, "log", "", "The log file to write to.")
	flag.IntVar(&width, "width", 1024, "The width to create the canvas.")
	flag.IntVar(&height, "height", 1024, "The height to create the canvas.")
	flag.IntVar(&count, "count", 64, "The maximum number of connections.")
	flag.IntVar(&saveInterval, "sinterval", 180, "Save interval in seconds.")
	flag.StringVar(&whitelistPath, "whitelist", "./whitelist.csv", "The path to a whitelist.")
	flag.StringVar(&loadRecordPath, "loadRecord", "", "The png to load as the record.")
	flag.StringVar(&saveRecordPath, "saveRecord", "./record.png", "The path to save the record.")
	flag.BoolVar(&enableWL, "wl", false, "Enable whitelist.")
	flag.StringVar(&bucketUrl, "bucketUrl", "", "Supabase Bucket url")
	flag.StringVar(&apiUrl, "apiUrl", "", "Supabase API Url")
	flag.StringVar(&userEmail, "userEmail", "", "Authenticated user email")
	flag.StringVar(&userPassword, "userPassword", "", "Authenticated user password")
	flag.StringVar(&anonKey, "anonKey", "", "Supabase API ANON Key")
}

func main() {
	flag.Parse()

	if logPath != "" {
		f, err := os.OpenFile("place.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(f)
	}

	var img draw.Image
	if loadPath == "" {
		log.Printf("Creating new canvas with dimensions %d x %d\n", width, height)
		nrgba := image.NewNRGBA(image.Rect(0, 0, width, height))
		for i := range nrgba.Pix {
			nrgba.Pix[i] = 255
		}
		img = nrgba
	} else {
		log.Printf("Loading canvas from %s\n", loadPath)
		img = loadImage(loadPath)
	}

	var whitelist map[string]uint16
	var record draw.Image
	if enableWL {
		d, err := readWhitelist(whitelistPath)
		if err != nil {
			panic(err)
		}
		whitelist = d
		if loadRecordPath == "" {
			log.Printf("Creating new record image with dimensions %d x %d\n", width, height)
			record = image.NewGray16(image.Rect(0, 0, width, height))
		} else {
			log.Printf("Loading record image from %s\n", loadRecordPath)
			record = loadImage(loadRecordPath)
		}
	}

	placeSv := place.NewServer(img, count, enableWL, whitelist, record)
	defer os.WriteFile(savePath, placeSv.GetImageBytes(), 0644)
	defer func() {
		if enableWL {
			os.WriteFile(savePath, placeSv.GetRecordBytes(), 0644)
		}
	}()
	go func() {
		for {
			os.WriteFile(savePath, placeSv.GetImageBytes(), 0644)
			if enableWL {
				os.WriteFile(saveRecordPath, placeSv.GetRecordBytes(), 0644)
				uploadFileToBucket(saveRecordPath, "image")
			}

			uploadFileToBucket(savePath, "image")

			time.Sleep(time.Second * time.Duration(saveInterval))
		}
	}()
	fs := httpfilter.NewServer(root, "", map[string]httpfilter.OpFunc{
		"place": func(w http.ResponseWriter, req *http.Request, args ...string) {
			placeSv.ServeHTTP(w, req)
		},
	})
	server := http.Server{
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)), //disable HTTP/2
		Addr:         port,
		Handler:      fs,
	}
	log.Fatal(server.ListenAndServe())
}

func loadImage(loadPath string) draw.Image {
	//Download file from Supabase
	url := bucketUrl + loadPath

	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Println("failed to download file, status: %s", resp.Status)
		fmt.Println("Creating new canvas with dimensions %d x %d\n", width, height)
		nrgba := image.NewNRGBA(image.Rect(0, 0, width, height))
		for i := range nrgba.Pix {
			nrgba.Pix[i] = 255
		}
		return (nrgba)
	}

	file, err := os.Create(loadPath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		panic(err)
	}

	// ---------

	f, err := os.Open(loadPath)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	pngimg, err := png.Decode(f)
	if err != nil {
		panic(err)
	}
	return pngimg.(draw.Image)
}

func readWhitelist(whitelistPath string) (map[string]uint16, error) {
	//Download file from Supabase
	url := bucketUrl + whitelistPath

	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("failed to download file, status: %s", resp.Status))
	}

	file, err := os.Create(whitelistPath)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		panic(err)
	}

	//------------------------

	f, err := os.Open(whitelistPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	csvReader := csv.NewReader(f)
	data, err := csvReader.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	whitelist := make(map[string]uint16)
	for line, v := range data {
		x, err := strconv.Atoi(v[1])
		if err != nil {
			panic(fmt.Sprintf("Error when reading whitelist on line %d: %s", line, err.Error()))
		}
		whitelist[v[0]] = uint16(x)
	}
	return whitelist, nil
}

func getJWTToken() string {

	// Prepare request body
	bodyData := map[string]string{
		"email":    userEmail,
		"password": userPassword,
	}
	bodyJSON, _ := json.Marshal(bodyData)

	// Create HTTP request
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(bodyJSON))
	if err != nil {
		fmt.Println("❌ Failed to create request:", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", anonKey)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("❌ Request error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Read and parse response
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Println("❌ Login failed:", string(body))
		os.Exit(1)
	}

	var authResp AuthResponse
	err = json.Unmarshal(body, &authResp)
	if err != nil {
		fmt.Println("❌ Failed to parse response:", err)
		os.Exit(1)
	}
	return (authResp.AccessToken)
}

func uploadFileToBucket(filename string, format string) {
	url := fmt.Sprintf("%s%s", bucketUrl, strings.TrimPrefix(filename, `./`))

	// Open the file to upload
	file, err := os.Open(savePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// Read the file's content into memory
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Fatal(err)
	}

	// Create a new HTTP POST request
	req, err := http.NewRequest("POST", url, bytes.NewReader(fileBytes))
	if err != nil {
		log.Fatal(err)
	}

	// Add the required headers
	req.Header.Add("Authorization", getJWTToken())
	if format == "image" {
		req.Header.Add("Content-Type", "image/png")
	}
	if format == "text" {
		req.Header.Add("Content-Type", "text/plain")
	}
	req.Header.Add("x-upsert", "true")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Println("✅ File uploaded to Supabase : " + filename)
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ Upload failed. Status: %s\nBody: %s\n", resp.Status, string(body))
	}
}
