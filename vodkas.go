//  Seems to work: `go build -ldflags "-linkmode external -extldflags -static"`
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

/**************** Handler Functions ****************/

const FormNameFile = "file"
const FormNameText = "text"
const FormNameNumShots = "numdls"

var KeyTakenMessage = []byte("The requested key is taken. Try another.\n")
// TODO: Might want to update this with info about vv.sh instead?
var InfoMessage = []byte(`Usage:
- POUR:     curl vetle.vodka[/<requested-shot-key>] -d <data>
- SHOT:     curl vetle.vodka/<shot-key>

A shot key is an at-the-moment unique ID that's linked to the dumped data.
As soon as a specific shot has been accessed both the link and the contents 
are removed completely.
`)

var rootBucket = "root"
var db *bbolt.DB

// Pops data from database. Will probably be replaced by shot(), and support more
// than a single download (although that will still be the default)
func pop(key string) (contents []byte, err error) {
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(rootBucket))
		if b == nil {
			return errors.New("Failed to open root bucket")
		}
		contents = b.Get([]byte(key))
		if contents != nil {
			fmt.Printf("Found contents for shotkey %v\n", key)
			return b.Delete([]byte(key))
		} else {
			return nil
		}
	})
	return
}

// Check if the key is taken without touching the contents
func smell(shotKey string) (found bool) {
	_ = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(rootBucket))
		if b == nil {
			log.Fatal("Failed to open root bucket")
		}
		found = b.Get([]byte(shotKey)) != nil
		return nil
	})
	return
}

func shot(shotKey string) (contents []byte, err error) {
	contents, err = pop(shotKey)
	return
}

func pour(shotKey string, r *http.Request) (err error) {
	var contents []byte
	var numshots int
	// Dumps can be both x-www-urlencoded and multipart/form-data.
	// Try multipart first, then x-www-urlencoded if no mpReader is returned
	mpReader, _ := r.MultipartReader()
	if mpReader != nil {
		contents, numshots, err = extractMultipart(mpReader)
	} else {
		numshots = 1
		contents, err = ioutil.ReadAll(r.Body)
	}
	if err != nil {
		return err
	}
	fmt.Printf("Number of shots: %v", numshots)
	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket([]byte(rootBucket))
		if b == nil {
			return errors.New("Failed to open root bucket")
		}
		return b.Put([]byte(shotKey), contents)
	})
	return err
}

func extractMultipart(mr *multipart.Reader) (contents []byte, num int, err error) {
	for {
		var part *multipart.Part
		part, err = mr.NextPart()
		if err == io.EOF {
			err = nil
			break
		}
		formName := part.FormName()
		if err != nil {
			log.Fatal(err)
		}
		if formName == FormNameText || formName == FormNameFile {
			contents, err = ioutil.ReadAll(part)
			if err != nil {
				return
			}
			continue
		}
		if formName == FormNameNumShots {
			var numShotsRaw []byte
			numShotsRaw, err = ioutil.ReadAll(part)
			if err != nil {
				return
			}
			num, err = strconv.Atoi(string(numShotsRaw))
			if err != nil {
				return
			}
		}
	}
	err = nil
	return
}

// TODO: Extract post page too
func writeUploadPage(res http.ResponseWriter, textOnly bool, shotKey string) (err error) {
	if textOnly {
		_, err = res.Write(InfoMessage)
	} else {
		templateData := struct{ ShotKey string }{shotKey}
		err = uploadPageTemplate.Execute(res, templateData)
	}
	if err != nil {
		res.WriteHeader(http.StatusInternalServerError)
	}
	return
}

// TODO: Make function that handles responses based on mode?? like
//              makeResponse(rw *http.ResponseWriter, textOnly bool, data, responseKey)
//       where textOnly and responseKey maps to response messages or templates

func RootHandler(res http.ResponseWriter, r *http.Request) {
	// Detect whether Simple mode (text only) is active
	textOnly := r.Header.Get("Simple") != "" // for forcing textOnly mode
	textOnly = textOnly || strings.Contains(r.Header.Get("User-Agent"), "curl")
	if r.Method == http.MethodGet {
		writeUploadPage(res, textOnly, "")
	} else if r.Method == http.MethodPost {
		// Generate random shot key
		random := make([]byte, 16)
		rand.Read(random)
		shotKey := hex.EncodeToString(random)
		// Try to pour
		if err := pour(shotKey, r); err != nil {
			log.Println(err)
			res.WriteHeader(http.StatusInternalServerError)
		}
		if /*textOnly*/ true {
			response := r.Host + "/" + shotKey
			if _, err := res.Write([]byte(response)); err != nil {
				log.Panicln("Error when trying to write response body")
			}
		}
	}
}

func KeyHandler(res http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["shotKey"]
	textOnly := r.Header.Get("Simple") != "" // for forcing textOnly mode
	textOnly = textOnly || strings.Contains(r.Header.Get("User-Agent"), "curl")
	if r.Method == http.MethodGet {
		contents, err := shot(key)
		if err != nil {
			log.Panicln("Error when trying to read contents")
			res.WriteHeader(http.StatusInternalServerError)
		}
		if contents == nil {
			writeUploadPage(res, textOnly, key)
		}
		if _, err := res.Write(contents); err != nil {
			log.Panicln("Error when trying to write response")
			res.WriteHeader(http.StatusInternalServerError)
		}
	} else if r.Method == http.MethodPost {
		if smell(key) {
			// POSTs to taken  shouldn't happen often, so use textOnly always
			if _, err := res.Write(KeyTakenMessage); err != nil {
				res.WriteHeader(http.StatusInternalServerError)
			}
		} else {
			if err := pour(key, r); err != nil {
				res.WriteHeader(http.StatusInternalServerError)
			}
		}
	}
	// GET requests only
	/*if !found {
	          res.WriteHeader(http.StatusNotFound)
	          if _, err := res.Write([]byte(fmt.Sprint("404 no shot here\n"))); err != nil {
	                  log.Panicln("Error when trying to write response")
	          }
	          return
	  if found {
	          // For POST requests to specific key, this should actually return 'link taken' or something
	          if _, err := res.Write([]byte(contents)); err != nil {
	                  log.Panicln("Error when trying to write response body")
	          }
	  } else {
	          pour(key, r)
	          if _, err := res.Write([]byte(fmt.Sprint("Contents stored in given link\n"))); err != nil {
	                  log.Panicln("Error when trying to write response")
	          }
	  }*/
	fmt.Printf("Request from client: %v\n", r.Header.Get("User-Agent"))
	return
}

// Prints all keys in the database along with size of the contents
func statDB() {
	err := db.View(func(tx *bbolt.Tx) error {
		root := tx.Bucket([]byte(rootBucket))
		if root == nil {
			return errors.New("Failed to open root bucket")
		}
		number := 0
		fmt.Println("Elements in database:")
		err := root.ForEach(func(k, v []byte) error {
			fmt.Printf("%v %v\n", string(k), len(v))
			number++
			return nil
		})
		fmt.Printf("\n%v elements \n", number)
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
}

/**************** Main ****************/
func main() {
	port := flag.Int("p", 8080, "Port")
	dbFile := flag.String("d", "vodka.db", "Database file")
	stat := flag.Bool("s", false, "View database keys and size of associated contents")
	flag.Parse()
	var err error // Because ':=' can't be used on the line below without declaring db as a new *local* variable, making the global one nil
	db, err = bbolt.Open(*dbFile, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	defer db.Close()
	if err != nil {
		panic(err)
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(rootBucket))
		if err != nil {
			return err
		}
		return err
	})
	if err != nil {
		panic(err)
	}
	if *stat {
		statDB()
		return
	}

	fmt.Println("Server started listening at port", *port)
	router := mux.NewRouter()
	router.HandleFunc("/", RootHandler)
	router.HandleFunc("/{shotKey}", KeyHandler)
	http.Handle("/", router)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", *port), nil))
}
