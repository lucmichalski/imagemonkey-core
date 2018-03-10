package main

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"fmt"
	"github.com/satori/go.uuid"
	"gopkg.in/h2non/filetype.v1"
	log "github.com/Sirupsen/logrus"
	"flag"
	"database/sql"
	"os"
	"strconv"
	"github.com/oschwald/geoip2-golang"
	"github.com/garyburd/redigo/redis"
	"encoding/json"
	"net"
	"errors"
	"html"
	"net/url"
	"image"
	"github.com/nfnt/resize"
	"bytes"
	"image/gif"
    "image/jpeg"
    "image/png"
    "strings"
    "encoding/base64"
    "github.com/dgrijalva/jwt-go"
    "golang.org/x/crypto/bcrypt"
    "time"
	//"gopkg.in/h2non/bimg.v1"
)

var db *sql.DB
var geoipDb *geoip2.Reader


func ResizeImage(path string, width uint, height uint) ([]byte, string, error){
	buf := new(bytes.Buffer) 
	imgFormat := ""

	file, err := os.Open(path)
	if err != nil {
		log.Debug("[Resize Image Handler] Couldn't open image: ", err.Error())
		return buf.Bytes(), imgFormat, err
	}

	// decode jpeg into image.Image
	img, format, err := image.Decode(file)
	if err != nil {
		log.Debug("[Resize Image Handler] Couldn't decode image: ", err.Error())
		return buf.Bytes(), imgFormat, err
	}
	file.Close()

	resizedImg := resize.Resize(width, height, img, resize.NearestNeighbor)


	if format == "png" {
		err = png.Encode(buf, resizedImg)
		if err != nil {
			log.Debug("[Resize Image Handler] Couldn't encode image: ", err.Error())
	    	return buf.Bytes(), imgFormat, err
		}
	} else if format == "gif" {
		err = gif.Encode(buf, resizedImg, nil)
		if err != nil {
			log.Debug("[Resize Image Handler] Couldn't encode image: ", err.Error())
	    	return buf.Bytes(), imgFormat, err
		}
	} else {
		err = jpeg.Encode(buf, resizedImg, nil)
		if err != nil {
			log.Debug("[Resize Image Handler] Couldn't encode image: ", err.Error())
	    	return buf.Bytes(), imgFormat, err
		}
	}
	imgFormat = format

	return buf.Bytes(), imgFormat, nil
}

//Middleware to ensure that the correct X-Client-Id and X-Client-Secret are provided in the header
func ClientAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		var clientId string
		var clientSecret string

		clientId = ""
		if values, _ := c.Request.Header["X-Client-Id"]; len(values) > 0 {
			clientId = values[0]
		}

		clientSecret = ""
		if values, _ := c.Request.Header["X-Client-Secret"]; len(values) > 0 {
			clientSecret = values[0]
		}

		if(!((clientSecret == X_CLIENT_SECRET) && (clientId == X_CLIENT_ID))) {
			c.String(401, "Please provide a valid client id and client secret")
			c.AbortWithStatus(401)
			return
		}

		c.Next()
	}
}

//CORS Middleware 
func CorsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	    c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-Id, Cache-Control, X-Requested-With, X-Browser-Fingerprint, X-App-Identifier, Authorization")
	    c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH")

		if c.Request.Method == "OPTIONS" {
             c.AbortWithStatus(200)
         } else {
             c.Next()
         }
	}
}

func RequestId() gin.HandlerFunc {
	return func(c *gin.Context) {
		if values, _ := c.Request.Header["X-Request-Id"]; len(values) > 0 {
			if(values[0] != ""){
				c.Writer.Header().Set("X-Request-Id", values[0])
			}
		}

		c.Next()
	}
}


func pushCountryContributionToRedis(redisPool *redis.Pool, contributionsPerCountryRequest ContributionsPerCountryRequest) {
	serialized, err := json.Marshal(contributionsPerCountryRequest)
	if err != nil { 
		log.Debug("[Push Contributions per Country to Redis] Couldn't create contributions-per-country request: ", err.Error())
		return
	}

	redisConn := redisPool.Get()
	defer redisConn.Close()

	_, err = redisConn.Do("RPUSH", "contributions-per-country", serialized)
	if err != nil { //just log error, but not abort (it's just some statistical information)
		log.Debug("[Push Contributions per Country to Redis] Couldn't update contributions-per-country: ", err.Error())
		return
	}
}

func annotationsValid(annotations []json.RawMessage) error{
	for _, r := range annotations {
		var obj map[string]interface{}
        err := json.Unmarshal(r, &obj)
        if err != nil {
            return err
        }

        shapeType := obj["type"]
        if shapeType == "rect" {
        	var rectangleAnnotation RectangleAnnotation 
			err = json.Unmarshal(r, &rectangleAnnotation)
			if err != nil {
				return err
			}
        } else if shapeType == "ellipse" {
        	var ellipsisAnnotation EllipsisAnnotation 
			err = json.Unmarshal(r, &ellipsisAnnotation)
			if err != nil {
				return err
			}
        } else if shapeType == "polygon" {
        	var polygonAnnotation PolygonAnnotation 
			err = json.Unmarshal(r, &polygonAnnotation)
			if err != nil {
				return err
			}
        } else {
        	return errors.New("Invalid type")
        }
	}

	if len(annotations) == 0 {
		return errors.New("annotations missing")
	}

	return nil
}

func getBrowserFingerprint(c *gin.Context) string {
	browserFingerprint := ""
	if values, _ := c.Request.Header["X-Browser-Fingerprint"]; len(values) > 0 {
		browserFingerprint = values[0]
	}

	return browserFingerprint
}

func getAppIdentifier(c *gin.Context) string {
	appIdentifier := ""
	if values, _ := c.Request.Header["X-App-Identifier"]; len(values) > 0 {
		appIdentifier = values[0]
	}

	return appIdentifier
}

func isLabelValid(labelsMap map[string]LabelMapEntry, label string, sublabels []string) bool {
	if val, ok := labelsMap[label]; ok {
		if len(sublabels) > 0 {
			availableSublabels := val.LabelMapEntries

			for _, value := range sublabels {
				_, ok := availableSublabels[value]
				if !ok {
					return false
				}
			}
			return true
		}
		return true
	}

	return false
}

func IsFilenameValid(filename string) bool {
	_, err := uuid.FromString(filename)

	if err != nil {
		return false
	}
	return true
	/*for _, ch := range filename {
		if ((ch >= 'A') && (ch <= 'Z')) || ((ch >= 'a') && (ch <= 'z')) || ((ch >= '0') && (ch <= '9')) || (ch == '-') {
			continue
		}
		return false
	}
	return true*/
}


func main(){
	fmt.Printf("Starting API Service...\n")

	log.SetLevel(log.DebugLevel)

	releaseMode := flag.Bool("release", false, "Run in release mode")
	wordlistPath := flag.String("wordlist", "../wordlists/en/labels.json", "Path to label map")
	donationsDir := flag.String("donations_dir", "../donations/", "Location of the uploaded donations")
	unverifiedDonationsDir := flag.String("unverified_donations_dir", "../unverified_donations/", "Location of the uploaded but unverified donations")
	redisAddress := flag.String("redis_address", ":6379", "Address to the Redis server")
	redisMaxConnections := flag.Int("redis_max_connections", 500, "Max connections to Redis")
	geoIpDbPath := flag.String("geoip_db", "../geoip_database/GeoLite2-Country.mmdb", "Path to the GeoIP database")
	labelExamplesDir := flag.String("examples_dir", "../label_examples/", "Location of the label examples")
	userProfilePicturesDir := flag.String("avatars_dir", "../avatars/", "Avatars directory")

	flag.Parse()
	if(*releaseMode){
		fmt.Printf("[Main] Starting gin in release mode!\n")
		gin.SetMode(gin.ReleaseMode)
	}

	log.Debug("[Main] Reading Label Map")
	labelMap, words, err := getLabelMap(*wordlistPath)
	if err != nil {
		fmt.Printf("[Main] Couldn't read label map...terminating!")
		log.Fatal(err)
	}

	//if the mostPopularLabels gets extended with sublabels, 
	//adapt getRandomGroupedImages() and validateImages() also!
	mostPopularLabels := []string{"cat", "dog"}

	//open database and make sure that we can ping it
	db, err = sql.Open("postgres", IMAGE_DB_CONNECTION_STRING)
	if err != nil {
		log.Fatal("[Main] Couldn't open database: ", err.Error())
	}

	err = db.Ping()
	if err != nil {
		log.Fatal("[Main] Couldn't ping database: ", err.Error())
	}


	//open geoip database
	geoipDb, err := geoip2.Open(*geoIpDbPath)
	if err != nil {
		log.Fatal("[Main] Couldn't read geoip database: ", err.Error())
	}
	defer geoipDb.Close()

	//create redis pool
	redisPool := redis.NewPool(func() (redis.Conn, error) {
		c, err := redis.Dial("tcp", *redisAddress)

		if err != nil {
			log.Fatal("[Main] Couldn't dial redis: ", err.Error())
		}

		return c, err
	}, *redisMaxConnections)
	defer redisPool.Close()

	statisticsPusher := NewStatisticsPusher(redisPool)
	err = statisticsPusher.Load()
	if err != nil {
		log.Fatal("[Main] Couldn't load statistics pusher: ", err.Error())
	}

	sampleExportQueries := GetSampleExportQueries()

	authTokenHandler := NewAuthTokenHandler()


	router := gin.Default()
	router.Use(CorsMiddleware())
	router.Use(RequestId())

	//serve images in "donations" directory with the possibility to scale images
	//before serving them
	router.GET("/v1/donation/:imageid", func(c *gin.Context) {
		params := c.Request.URL.Query()
		imageId := c.Param("imageid")

		var width uint
		width = 0
		if temp, ok := params["width"]; ok {
			n, err := strconv.ParseUint(temp[0], 10, 32)
		    if err == nil {
		        width = uint(n)
		    }
		}

		var height uint
		height = 0
		if temp, ok := params["height"]; ok {
			n, err := strconv.ParseUint(temp[0], 10, 32)
		    if err == nil {
            	height = uint(n)
		    }
		}

		if !IsFilenameValid(imageId) {
			c.String(404, "Invalid filename")
			return
		} 

		imgBytes, format, err := ResizeImage((*donationsDir + imageId), width, height)
		if err != nil {
			log.Debug("[Serving Donation] Couldn't serve donation: ", err.Error())
			c.String(500, "Couldn't process request, please try again later")
			return

		}

		c.Writer.Header().Set("Content-Type", ("image/" + format))
        c.Writer.Header().Set("Content-Length", strconv.Itoa(len(imgBytes)))
        _, err = c.Writer.Write(imgBytes) 
        if err != nil {
            log.Debug("[Serving Donation] Couldn't serve donation: ", err.Error())
            c.String(500, "Couldn't process request, please try again later")
            return
        }
	})

	//the following endpoints are secured with a client id + client secret. 
	//that's mostly because currently each donation needs to be unlocked manually. 
	//(as we want to make sure that we don't accidentally host inappropriate content, like nudity)
	clientAuth := router.Group("/")
	clientAuth.Use(RequestId())
	clientAuth.Use(ClientAuthMiddleware())
	{
		clientAuth.Static("./v1/unverified/donation", *unverifiedDonationsDir)
		clientAuth.GET("/v1/unverified/donation", func(c *gin.Context) {
			images, err := getAllUnverifiedImages()
			use(images)
			if err != nil {
				c.JSON(500, gin.H{"error" : "Couldn't process request - please try again later"}) 
			} else {
				c.JSON(http.StatusOK, images)
			}
		})

		clientAuth.POST("/v1/internal/auto-annotate/:imageid",  func(c *gin.Context) {
			imageId := c.Param("imageid")
			if imageId == "" {
				c.JSON(422, gin.H{"error": "invalid request - image id missing"})
				return
			}

			var annotations Annotations
			err := c.BindJSON(&annotations)
			if err != nil {
				c.JSON(422, gin.H{"error": "invalid request - annotations missing"})
				return
			}

			err = annotationsValid(annotations.Annotations)
			if err != nil {
				c.JSON(422, gin.H{"error": "invalid request - annotations invalid"})
				return
			}

			var apiUser APIUser
			apiUser.ClientFingerprint = ""
			apiUser.Name = ""

			err = addAnnotations(apiUser, imageId, annotations, true)
			if(err != nil){
				c.JSON(500, gin.H{"error": "Couldn't add annotations - please try again later"})
				return
			}
			c.JSON(201, nil)
		})

		clientAuth.GET("/v1/internal/auto-annotation",  func(c *gin.Context) {
			params := c.Request.URL.Query()

			labelsStr := ""
			if temp, ok := params["label"]; ok {
				labelsStr = temp[0]
			}

			if labelsStr == "" {
				c.JSON(422, gin.H{"error": "Please provide at least one label"})
				return
			}

			labels := strings.Split(labelsStr, ",")

			images, err := getImagesForAutoAnnotation(labels)
			if err != nil {
				c.JSON(500, gin.H{"error": "Couldn't get images - please try again later"})
				return
			}

			c.JSON(http.StatusOK, images)
		})

		

		clientAuth.POST("/v1/unverified/donation/:imageid/:param",  func(c *gin.Context) {
			imageId := c.Param("imageid")

			//verify that uuid is a valid UUID (to prevent path injection)
			_, err := uuid.FromString(imageId)
			if err != nil {
				c.JSON(400, gin.H{"error" : "Couldn't process request - not a valid image id"})
				return
			}

			param := c.Param("param")

			if param == "good" {
				src := *unverifiedDonationsDir + imageId
				dst := *donationsDir + imageId
				err := os.Rename(src, dst)
				if err != nil {
					log.Debug("[Main] Couldn't move file ", src, " to ", dst)
					c.JSON(500, gin.H{"error" : "Couldn't process request - please try again later"})
					return
				}

				err = unlockImage(imageId)
				if err != nil {
					c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
					return
				}
			} else if(param == "bad") { //not handled at the moment, add later if needed

			} else if param == "delete" {
				err = deleteImage(imageId)
				if err != nil {
					c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
					return
				}

				dst := *unverifiedDonationsDir + imageId
				err := os.Remove(dst)
				if err != nil {
					log.Debug("[Main] Couldn't remove file ", dst)
					c.JSON(500, gin.H{"error" : "Couldn't process request - please try again later"})
					return
				}

			} else{
				c.JSON(404, gin.H{"error": "Couldn't process request - invalid parameter"})
				return
			}

			c.JSON(http.StatusOK, nil)
		})
	}

	router.GET("/v1/validate", func(c *gin.Context) {
		params := c.Request.URL.Query()

		grouped := false
		if temp, ok := params["grouped"]; ok {
			if temp[0] == "true" {
				grouped = true
			}
		}

		limit := 20
		if temp, ok := params["limit"]; ok {
			limit, err = strconv.Atoi(temp[0])
			if err != nil {
				c.JSON(400, gin.H{"error": "Couldn't process request - invalid limit parameter"})
				return
			}
		}

		if grouped {
			pos := 0
			if len(mostPopularLabels) > 0 {
				pos = random(0, (len(mostPopularLabels) - 1))
			}
				

			randomGroupedImages, err := getRandomGroupedImages(mostPopularLabels[pos], limit)
			if err != nil {
				c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"label": mostPopularLabels[pos], "donations": randomGroupedImages})

		} else {
			randomImage := getRandomImage()
			c.JSON(http.StatusOK, gin.H{"uuid": randomImage.Id, "label": randomImage.Label, "provider": randomImage.Provider, "sublabel": randomImage.Sublabel, 
										"validations": gin.H{ "num_yes": randomImage.NumOfValid, "num_no": randomImage.NumOfInvalid} })
		}
	})

	router.POST("/v1/donation/:imageid/labelme", func(c *gin.Context) {
		imageId := c.Param("imageid")

		var labels []LabelMeEntry
		if c.BindJSON(&labels) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - labels missing"})
			return
		}

		browserFingerprint := getBrowserFingerprint(c)

		for _, item := range labels {
			if !isLabelValid(labelMap, item.Label, item.Sublabels) {
				c.JSON(400, gin.H{"error": "Couldn't process request - invalid label(s)"})
				return
			}
		}

		err := addLabelsToImage(browserFingerprint, imageId, labels)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(http.StatusOK, nil)
	})

	router.POST("/v1/donation/:imageid/validate/:param", func(c *gin.Context) {
		imageId := c.Param("imageid")
		param := c.Param("param")

		parameter := false
		if param == "yes" {
			parameter = true
		} else if param == "no" {
			parameter = false
		} else{
			c.JSON(404, nil)
			return
		}

		var labelValidationEntry LabelValidationEntry
		if c.BindJSON(&labelValidationEntry) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - please provide valid label(s)"})
			return
		}

		if ((labelValidationEntry.Label == "") && (labelValidationEntry.Sublabel == "")) {
			c.JSON(400, gin.H{"error": "Please provide a valid label"})
			return
		}


		var apiUser APIUser
		apiUser.ClientFingerprint = getBrowserFingerprint(c)
		apiUser.Name = authTokenHandler.GetAccessTokenInfo(c).Username

		err := validateDonatedPhoto(apiUser, imageId, labelValidationEntry, parameter)
		if(err != nil){
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Couldn't process request - please try again later"})
			return
		} 

		//get client IP address and try to determine country
		var contributionsPerCountryRequest ContributionsPerCountryRequest
		contributionsPerCountryRequest.Type = "validation"
		contributionsPerCountryRequest.CountryCode = "--"
		ip := net.ParseIP(getIPAddress(c.Request))
		if ip != nil {
			record, err := geoipDb.Country(ip)
			if err != nil { //just log, but don't abort...it's just for statistics
				log.Debug("[Validation] Couldn't determine geolocation from ", err.Error())
				
			} else {
				contributionsPerCountryRequest.CountryCode = record.Country.IsoCode
			}
		}
		pushCountryContributionToRedis(redisPool, contributionsPerCountryRequest)
		statisticsPusher.PushAppAction(getAppIdentifier(c), "validation")

		c.JSON(http.StatusOK, nil)
	})

	router.GET("/v1/labelme", func(c *gin.Context) {
		image, err := getImageToLabel()
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"uuid": image.Id, "provider": image.Provider, "all_labels": image.AllLabels})
	})

	router.PATCH("/v1/donation/validate", func(c *gin.Context) {
		var imageValidationBatch ImageValidationBatch
		
		if c.BindJSON(&imageValidationBatch) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - invalid patch"})
			return
		}

		browserFingerprint := getBrowserFingerprint(c)

		err := validateImages(browserFingerprint, imageValidationBatch)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}

		//get client IP address and try to determine country
		var contributionsPerCountryRequest ContributionsPerCountryRequest
		contributionsPerCountryRequest.Type = "validation"
		contributionsPerCountryRequest.CountryCode = "--"
		ip := net.ParseIP(getIPAddress(c.Request))
		if ip != nil {
			record, err := geoipDb.Country(ip)
			if err != nil { //just log, but don't abort...it's just for statistics
				log.Debug("[Validation] Couldn't determine geolocation from ", err.Error())
				
			} else {
				contributionsPerCountryRequest.CountryCode = record.Country.IsoCode
			}
		}
		pushCountryContributionToRedis(redisPool, contributionsPerCountryRequest)
		statisticsPusher.PushAppAction(getAppIdentifier(c), "validation")

		c.JSON(200, nil)
	})

	router.GET("/v1/export", func(c *gin.Context) {
		params := c.Request.URL.Query()

		annotationsOnly := false
		if temp, ok := params["annotations_only"]; ok {
			if temp[0] == "true" {
				annotationsOnly = true
			}
		}

		if temp, ok := params["query"]; ok {
			if temp[0] == "" {
				c.JSON(422, gin.H{"error": "no query specified"})
				return
			}


			query, err := url.QueryUnescape(temp[0])
			if err != nil {
				c.JSON(422, gin.H{"error": "invalid query"})
				return
			}

			queryParser := NewQueryParser(query)
			parseResult, err := queryParser.Parse()
			if err != nil {
				c.JSON(422, gin.H{"error": err.Error()})
				return
			}

			jsonData, err := export(parseResult, annotationsOnly)
			if(err == nil){
				c.JSON(http.StatusOK, jsonData)
				return
			} else{
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Couldn't export data, please try again later."})
				return
			}
		} else {
			c.JSON(422, gin.H{"error": "no query specified"})
			return
		}
	})


	router.GET("/v1/export/sample", func(c *gin.Context) {
		q := sampleExportQueries[random(0, len(sampleExportQueries) - 1)]
		c.JSON(http.StatusOK, q)
	})

	router.GET("/v1/label", func(c *gin.Context) {
		params := c.Request.URL.Query()
		if temp, ok := params["detailed"]; ok {
			if temp[0] == "true" {
				c.JSON(http.StatusOK, labelMap)
				return
			}
		}
		c.JSON(http.StatusOK, words)
	})

	/*router.GET("/v1/label/search", func(c *gin.Context) {
		params := c.Request.URL.Query()
		if temp, ok := params["q"]; ok {
			res, err := autocompleteLabel(temp[0])
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"Error": "Couldn't process request, please try again later."})
				return
			}
			c.JSON(http.StatusOK, res)
			return
		}

		c.JSON(422, gin.H{"error": "please provide a search query"})
	})*/


	router.GET("/v1/label/random", func(c *gin.Context) {
		label := words[random(0, len(words) - 1)]
		c.JSON(http.StatusOK, gin.H{"label": label})
	})

	router.Static("/v1/label/example", *labelExamplesDir)		

	router.POST("/v1/label/suggest", func(c *gin.Context) {
		type SuggestedLabel struct {
		    Name string `json:"label"`
		}
		var suggestedLabel SuggestedLabel

		if c.BindJSON(&suggestedLabel) != nil {
			c.JSON(422, gin.H{"error": "Couldn't process request - label missing"})
			return
		}

		escapedLabel := html.EscapeString(suggestedLabel.Name)
		err = addLabelSuggestion(escapedLabel)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}

		c.JSON(200, nil)
	})

	/*router.GET("/v1/label/suggest", func(c *gin.Context) {
		tags := ""
		params := c.Request.URL.Query()
		temp, ok := params["tags"] 
		if !ok {
			c.JSON(422, gin.H{"error": "Couldn't process request - no tags specified"})
			return
		}
		tags = temp[0]
		suggestedTags, err := getLabelSuggestions(tags)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(http.StatusOK, suggestedTags)
	})*/

	router.GET("/v1/label/popular", func(c *gin.Context) {
		popularLabels, err := getMostPopularLabels(10) //limit to 10
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(http.StatusOK, popularLabels)
	})



	router.POST("/v1/donate", func(c *gin.Context) {
		label := c.PostForm("label")

		file, header, err := c.Request.FormFile("image")
		if(err != nil){
			fmt.Printf("err = %s", err.Error())
			c.JSON(400, gin.H{"error": "Picture is missing"})
			return
		}

		// Create a buffer to store the header of the file
        fileHeader := make([]byte, 512)

        // Copy the file header into the buffer
        if _, err := file.Read(fileHeader); err != nil {
        	c.JSON(422, gin.H{"error": "Unable to detect MIME type"})
        	return
        }

        // set position back to start.
        if _, err := file.Seek(0, 0); err != nil {
        	c.JSON(422, gin.H{"error": "Unable to detect MIME type"})
        	return
        }

        if(!filetype.IsImage(fileHeader)){
        	c.JSON(422, gin.H{"error": "Unsopported MIME type detected"})
        	return
        }

        //check if image already exists by using an image hash
        imageInfo, err := getImageInfo(file)
        if(err != nil){
        	c.JSON(500, gin.H{"error": "Couldn't add photo - please try again later"})
        	return 
        }
        exists, err := imageExists(imageInfo.Hash)
        if(err != nil){
        	c.JSON(500, gin.H{"error": "Couldn't add photo - please try again later"})
        	return
        }
        if(exists){
        	c.JSON(409, gin.H{"error": "Couldn't add photo - image already exists"})
        	return
        }

        if label != "" { //allow unlabeled donation. If label is provided it needs to be valid!
        	if !isLabelValid(labelMap, label, []string{}) {
        		c.JSON(409, gin.H{"error": "Couldn't add photo - invalid label"})
        		return
        	}
        }

        addSublabels := false
        temp := c.PostForm("add_sublabels")
        if temp == "true" {
        	addSublabels = true
        }


		labelMapEntry := labelMap[label]
		if !addSublabels {
			labelMapEntry.LabelMapEntries = nil
		}
		var labelMeEntry LabelMeEntry
		var labelMeEntries []LabelMeEntry
		labelMeEntry.Label = label
		for key, _ := range labelMapEntry.LabelMapEntries {
			labelMeEntry.Sublabels = append(labelMeEntry.Sublabels, key)
		}
		labelMeEntries = append(labelMeEntries, labelMeEntry)

        //image doesn't already exist, so save it and add it to the database
		uuid := uuid.NewV4().String()
		err = c.SaveUploadedFile(header, (*unverifiedDonationsDir + uuid))
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't set add photo - please try again later"})	
			return
		}

		browserFingerprint := getBrowserFingerprint(c)

		err = addDonatedPhoto(browserFingerprint, uuid, imageInfo, labelMeEntries)
		if(err != nil){
			c.JSON(500, gin.H{"error": "Couldn't add photo - please try again later"})	
			return
		}

		//get client IP address and try to determine country
		var contributionsPerCountryRequest ContributionsPerCountryRequest
		contributionsPerCountryRequest.Type = "donation"
		contributionsPerCountryRequest.CountryCode = "--"
		ip := net.ParseIP(getIPAddress(c.Request))
		if ip != nil {
			record, err := geoipDb.Country(ip)
			if err != nil { //just log, but don't abort...it's just for statistics
				log.Debug("[Donation] Couldn't determine geolocation from ", err.Error())
				
			} else {
				contributionsPerCountryRequest.CountryCode = record.Country.IsoCode
			}
		}
		pushCountryContributionToRedis(redisPool, contributionsPerCountryRequest)
		statisticsPusher.PushAppAction(getAppIdentifier(c), "donation")

		c.JSON(http.StatusOK, nil)
	})

	router.POST("/v1/report/:imageid", func(c *gin.Context) {
		imageId := c.Param("imageid")

		var report Report
		if(c.BindJSON(&report) != nil){
			c.JSON(422, gin.H{"error": "reason missing - please provide a valid 'reason'"})
			return
		}
		err := reportImage(imageId, report.Reason)
		if(err != nil){
			c.JSON(500, gin.H{"error": "Couldn't report image - please try again later"})
			return
		}
		c.JSON(http.StatusOK, nil)
	})

	router.POST("/v1/annotation/:annotationid/validate/:param", func(c *gin.Context) {
		annotationId := c.Param("annotationid")
		param := c.Param("param")

		parameter := false
		if param == "yes" {
			parameter = true
		} else if param == "no" {
			parameter = false
		} else{
			c.JSON(404, nil)
			return
		}

		var labelValidationEntry LabelValidationEntry
		if c.BindJSON(&labelValidationEntry) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - please provide valid label(s)"})
			return
		}

		if ((labelValidationEntry.Label == "") && (labelValidationEntry.Sublabel == "")) {
			c.JSON(400, gin.H{"error": "Please provide a valid label"})
			return
		}

		browserFingerprint := getBrowserFingerprint(c)

		err := validateAnnotatedImage(browserFingerprint, annotationId, labelValidationEntry, parameter)
		if(err != nil){
			c.JSON(http.StatusInternalServerError, gin.H{"Error": "Database Error: Couldn't update data"})
			return
		} 

		//get client IP address and try to determine country
		var contributionsPerCountryRequest ContributionsPerCountryRequest
		contributionsPerCountryRequest.Type = "validation"
		contributionsPerCountryRequest.CountryCode = "--"
		ip := net.ParseIP(getIPAddress(c.Request))
		if ip != nil {
			record, err := geoipDb.Country(ip)
			if err != nil { //just log, but don't abort...it's just for statistics
				log.Debug("[Annotation] Couldn't determine geolocation from ", err.Error())
				
			} else {
				contributionsPerCountryRequest.CountryCode = record.Country.IsoCode
			}
		}
		pushCountryContributionToRedis(redisPool, contributionsPerCountryRequest)
		statisticsPusher.PushAppAction(getAppIdentifier(c), "validation")

		c.JSON(http.StatusOK, nil)
	})

	router.POST("/v1/annotate/:imageid", func(c *gin.Context) {
		imageId := c.Param("imageid")
		if imageId == "" {
			c.JSON(422, gin.H{"error": "invalid request - image id missing"})
			return
		}

		var annotations Annotations
		err := c.BindJSON(&annotations)
		if err != nil {
			c.JSON(422, gin.H{"error": "invalid request - annotations missing"})
			return
		}

		err = annotationsValid(annotations.Annotations)
		if err != nil {
			c.JSON(422, gin.H{"error": "invalid request - annotations invalid"})
			return
		}

		var apiUser APIUser
		apiUser.ClientFingerprint = getBrowserFingerprint(c)
		apiUser.Name = authTokenHandler.GetAccessTokenInfo(c).Username


		err = addAnnotations(apiUser, imageId, annotations, false)
		if(err != nil){
			c.JSON(500, gin.H{"error": "Couldn't add annotations - please try again later"})
			return
		}


		//get client IP address and try to determine country
		var contributionsPerCountryRequest ContributionsPerCountryRequest
		contributionsPerCountryRequest.Type = "annotation"
		contributionsPerCountryRequest.CountryCode = "--"
		ip := net.ParseIP(getIPAddress(c.Request))
		if ip != nil {
			record, err := geoipDb.Country(ip)
			if err != nil { //just log, but don't abort...it's just for statistics
				log.Debug("[Annotate] Couldn't determine geolocation from ", err.Error())
				
			} else {
				contributionsPerCountryRequest.CountryCode = record.Country.IsoCode
			}
		}
		pushCountryContributionToRedis(redisPool, contributionsPerCountryRequest)
		statisticsPusher.PushAppAction(getAppIdentifier(c), "annotation")
	})

	router.GET("/v1/annotate", func(c *gin.Context) {
		params := c.Request.URL.Query()
		

		addAutoAnnotations := false
		if temp, ok := params["add_auto_annotations"]; ok {
			if temp[0] == "true" {
				addAutoAnnotations = true
			}
		}

		var labelId int64
		labelId = -1
		if temp, ok := params["label_id"]; ok {
			labelId, err = strconv.ParseInt(temp[0], 10, 64)
			if err != nil {
				c.JSON(422, gin.H{"error": "label id needs to be an integer"})
				return
			}
		}

		randomImage, err := getRandomUnannotatedImage(addAutoAnnotations, labelId)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(http.StatusOK, randomImage)
	})

	router.GET("/v1/annotation", func(c *gin.Context) {
		params := c.Request.URL.Query()
		

		autoGenerated := false
		if temp, ok := params["auto_generated"]; ok {
			if temp[0] == "true" {
				autoGenerated = true
			}
		}


		randomAnnotatedImage, err := getRandomAnnotatedImage(autoGenerated)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"image_uuid": randomAnnotatedImage.ImageId, "label": randomAnnotatedImage.Label, "provider": randomAnnotatedImage.Provider, 
									"annotations": randomAnnotatedImage.Annotations, "sublabel": randomAnnotatedImage.Sublabel, "annotation_uuid": randomAnnotatedImage.AnnotationId,
									"validations": gin.H{ "num_yes": randomAnnotatedImage.NumOfValid, "num_no": randomAnnotatedImage.NumOfInvalid}})
	})

	router.GET("/v1/annotation/refine", func(c *gin.Context) {
		randomImage,_ := getRandomAnnotationForRefinement()
		c.JSON(200, randomImage)
	})

	router.POST("/v1/annotation/:annotationid/refine/:annotationdataid", func(c *gin.Context) {
		annotationId := c.Param("annotationid")
		if(annotationId == ""){
			c.JSON(422, gin.H{"error": "Invalid request - please provide a valid annotation id"})
			return
		}

		temp := c.Param("annotationdataid")
		if(temp == ""){
			c.JSON(422, gin.H{"error": "Invalid request - please provide a valid annotation data id"})
			return
		}

		var annotationDataId int64
		annotationDataId, err = strconv.ParseInt(temp, 10, 64)
		if err != nil {
			c.JSON(422, gin.H{"error": "Invalid request - please provide a valid annotation data id"})
			return
		}


		var annotationRefinementEntries []AnnotationRefinementEntry
		if c.BindJSON(&annotationRefinementEntries) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - please provide a valid label id"})
			return
		}

		browserFingerprint := getBrowserFingerprint(c)

		err := addOrUpdateRefinements(annotationId, annotationDataId, annotationRefinementEntries, browserFingerprint)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"error": "Couldn't add annotation refinement - please try again later"})
			return
		}

		c.JSON(http.StatusOK, nil)
	})

	router.POST("/v1/blog/subscribe", func(c *gin.Context) {
		var blogSubscribeRequest BlogSubscribeRequest
		if c.BindJSON(&blogSubscribeRequest) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - please provide a valid email address"})
			return
		}


		redisConn := redisPool.Get()
		defer redisConn.Close()

		serialized, err := json.Marshal(blogSubscribeRequest)
		if err != nil { 
			log.Debug("[Subscribe to blog] Couldn't create subscribe-to-blog request: ", err.Error())
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}


		_, err = redisConn.Do("RPUSH", "subscribe-to-blog", serialized)
		if err != nil {
			log.Debug("[Subscribe to blog] Couldn't subscribe to blog: ", err.Error())
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}

		c.JSON(http.StatusOK, nil)
	})

	router.POST("/v1/login", func(c *gin.Context) {
		type MyCustomClaims struct {
			Username string `json:"username"`
			Created int64 `json:"created"`
			jwt.StandardClaims
		}


		auth := strings.SplitN(c.Request.Header.Get("Authorization"), " ", 2)

        if len(auth) != 2 || auth[0] != "Basic" {
            c.JSON(422, gin.H{"error": "Authorization failed"})
            return
        }

        payload, err := base64.StdEncoding.DecodeString(auth[1])
        if err != nil {
        	c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
        }
		pair := strings.SplitN(string(payload), ":", 2)


		hashedPassword, err := getHashedPasswordForUser(pair[0])
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		err = bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(pair[1]))
		if err == nil { //nil means password match
			now := time.Now()
			expirationTime := now.Add(time.Hour * 24 * 7)

			claims := MyCustomClaims{
				pair[0],
				now.Unix(),
				jwt.StandardClaims{
					ExpiresAt: expirationTime.Unix(),
					Issuer: "imagemonkey-api",
				},
			}

			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

			tokenString, err := token.SignedString([]byte(JWT_SECRET))
			if err != nil {
				c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            	return
			}

			err = addAccessToken(pair[0], tokenString, expirationTime.Unix())
			if err != nil {
				c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            	return
			}


			c.JSON(http.StatusOK, gin.H{"token": tokenString})
			return
		}

		c.JSON(403, nil)
	})

	router.POST("/v1/logout", func(c *gin.Context) {
		accessTokenInfo := authTokenHandler.GetAccessTokenInfo(c)
		err := removeAccessToken(accessTokenInfo.Token)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		c.JSON(200, nil)
	})

	router.POST("/v1/signup", func(c *gin.Context) {
		var userSignupRequest UserSignupRequest
		
		if c.BindJSON(&userSignupRequest) != nil {
			c.JSON(400, gin.H{"error": "Couldn't process request - invalid data"})
			return
		}

		if ((userSignupRequest.Username == "") || (userSignupRequest.Password == "") || (userSignupRequest.Email == "")) {
			c.JSON(422, gin.H{"error": "Invalid data"})
            return
		}

		if(!isAlphaNumeric(userSignupRequest.Username)){
			c.JSON(422, gin.H{"error": "Username contains invalid characters"})
            return
		}

		userExists, err := userExists(userSignupRequest.Username)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		if userExists {
			c.JSON(409, gin.H{"error": "Username already taken"})
            return
		}

		emailExists, err := emailExists(userSignupRequest.Email)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		if emailExists {
			c.JSON(409, gin.H{"error": "There already exists a username with this email address"})
            return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(userSignupRequest.Password), bcrypt.DefaultCost)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		err = createUser(userSignupRequest.Username, hashedPassword, userSignupRequest.Email)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		c.JSON(201, nil)
	})

	router.GET("/v1/user/:username/profile", func(c *gin.Context) {
		username := c.Param("username")
		if username == "" {
			c.JSON(422, gin.H{"error": "Invalid request - username missing"})
			return
		}

		userExists, err := userExists(username) 
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		if !userExists {
			c.JSON(422, gin.H{"error": "Invalid request - username doesn't exist"})
			return
		}

		userStatistics, err := getUserStatistics(username)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
            return
		}

		c.JSON(200, gin.H{"statistics": userStatistics})
	})

	router.POST("/v1/user/:username/password_reset", func(c *gin.Context) {
		username := c.Param("username")
		if username == "" {
			c.JSON(422, gin.H{"error": "Invalid request - username missing"})
			return
		}

		c.JSON(201, nil)
	})

	router.POST("/v1/user/:username/profile/change_picture", func(c *gin.Context) {
		username := c.Param("username")
		if username == "" {
			c.JSON(422, gin.H{"error": "Invalid request - username missing"})
			return
		}

		accessTokenInfo := authTokenHandler.GetAccessTokenInfo(c)
		if !accessTokenInfo.Valid {
			c.JSON(403, gin.H{"error": "Please provide a valid access token"})
			return
		}

		if accessTokenInfo.Username != username {
			c.JSON(403, gin.H{"error": "Permission denied"})
			return
		}

		file, header, err := c.Request.FormFile("image")
		if(err != nil){
			c.JSON(400, gin.H{"error": "Picture is missing"})
			return
		}

		// Create a buffer to store the header of the file
        fileHeader := make([]byte, 512)

        // Copy the file header into the buffer
        if _, err := file.Read(fileHeader); err != nil {
        	c.JSON(422, gin.H{"error": "Unable to detect MIME type"})
        	return
        }

        // set position back to start.
        if _, err := file.Seek(0, 0); err != nil {
        	c.JSON(422, gin.H{"error": "Unable to detect MIME type"})
        	return
        }

        if(!filetype.IsImage(fileHeader)){
        	c.JSON(422, gin.H{"error": "Unsopported MIME type detected"})
        	return
        }

        uuid := uuid.NewV4().String()
		err = c.SaveUploadedFile(header, (*userProfilePicturesDir + uuid))
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't set profile picture - please try again later"})	
			return
		}

		oldProfilePicture, err := changeProfilePicture(username, uuid)
		if(err != nil){
			c.JSON(500, gin.H{"error": "Couldn't set profile picture - please try again later"})	
			return
		}

		//if there exists an old profile picture, remove it.
		//we don't care if we can't remove it for some reason (it's not worth to fail the request just because of that) 
		if oldProfilePicture != "" {
			dst := *userProfilePicturesDir + oldProfilePicture
			os.Remove(dst)
		}

		c.JSON(201, nil)

	})

	router.GET("/v1/user/:username/profile/avatar", func(c *gin.Context) {
		params := c.Request.URL.Query()
		username := c.Param("username")
		if username == "" {
			c.JSON(422, gin.H{"error": "Invalid request - username missing"})
			return
		}

		userInfo, err := getUserInfo(username)
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}

		if userInfo.Name == "" {
			c.JSON(422, gin.H{"error": "Incorrect username"})
			return
		}


		var width uint
		width = 0
		if temp, ok := params["width"]; ok {
			n, err := strconv.ParseUint(temp[0], 10, 32)
		    if err == nil {
		        width = uint(n)
		    }
		}

		var height uint
		height = 0
		if temp, ok := params["height"]; ok {
			n, err := strconv.ParseUint(temp[0], 10, 32)
		    if err == nil {
            	height = uint(n)
		    }
		}

		fname := userInfo.ProfilePicture
		if userInfo.ProfilePicture == "" {
			fname = "default.png"
		}

		imgBytes, format, err := ResizeImage((*userProfilePicturesDir + fname), width, height)
		if err != nil {
			log.Debug("[Serving Avatar] Couldn't serve avatar: ", err.Error())
			c.String(500, "Couldn't process request - please try again later")
			return

		}

		c.Writer.Header().Set("Content-Type", ("image/" + format))
        c.Writer.Header().Set("Content-Length", strconv.Itoa(len(imgBytes)))
        _, err = c.Writer.Write(imgBytes) 
        if err != nil {
            log.Debug("[Serving Avatar] Couldn't serve avatar: ", err.Error())
            c.String(500, "Couldn't process request - please try again later")
            return
        }
	})


	router.GET("/v1//statistics/annotation", func(c *gin.Context) {
		//currently only last-month is allowed as period
		statistics, err := getAnnotationStatistics("last-month")
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(200, gin.H{"statistics": statistics, "period": "last-month"})
	})

	router.GET("/v1//statistics/validation", func(c *gin.Context) {
		//currently only last-month is allowed as period
		statistics, err := getValidationStatistics("last-month")
		if err != nil {
			c.JSON(500, gin.H{"error": "Couldn't process request - please try again later"})
			return
		}
		c.JSON(200, gin.H{"statistics": statistics, "period": "last-month"})
	})


	router.Run(":8081")
}