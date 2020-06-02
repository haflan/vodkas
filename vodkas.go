//  Seems to work: `go build -ldflags "-linkmode external -extldflags -static"`
package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"
)

/**************** Handler Functions ****************/

const (
	Version          = "1.0"
	FormNameFile     = "file"
	FormNameText     = "text"
	FormNameNumShots = "numdls"
	AdminKeyHeader   = "Admin-Key"
)

var (
	keyTakenMessage    = []byte("The requested key is taken. Try another.\n")
	serverErrorMessage = []byte("Internal server error!")
	pourSuccessMessage = []byte("Successfully submitted data")
)

// TODO: Might want to update this with info about vv.sh instead?
var InfoMessage = []byte(`Usage:
- POUR:     curl vetle.vodka[/<requested-shot-key>] -d <data>
- SHOT:     curl vetle.vodka/<shot-key>

A shot key is an at-the-moment unique ID that's linked to the dumped data.
As soon as a specific shot has been accessed both the link and the contents 
are removed completely.
`)

var db *bbolt.DB
var dataBucketKey = "data"
var numsBucketKey = "nums"
var shotNumsLimit int
var adminKey string
var storageCTRL struct {
	bytesMax  int
	bytesUsed int
	sync.Mutex
}

// Check if the key is taken without touching the contents
func smell(shotKey string) (found bool) {
	_ = db.View(func(tx *bbolt.Tx) error {
		nb := tx.Bucket([]byte(numsBucketKey))
		if nb == nil {
			log.Fatal("Failed to open essential bucket")
		}
		found = nb.Get([]byte(shotKey)) != nil
		return nil
	})
	return
}

// take a shot, i.e. load the contents and decrement the nums
func shot(shotKey string) (contents []byte, err error) {
	storageCTRL.Lock()
	defer storageCTRL.Unlock()
	err = db.Update(func(tx *bbolt.Tx) error {
		bShotKey := []byte(shotKey)
		datab := tx.Bucket([]byte(dataBucketKey))
		numsb := tx.Bucket([]byte(numsBucketKey))
		if datab == nil || numsb == nil {
			log.Fatal("Failed to open essential bucket")
		}
		// Find if key exists and, in that case, find number of shots left
		bnums := numsb.Get(bShotKey)
		if bnums == nil {
			return errors.New("no shots available for key " + shotKey)
		}
		nums, _ := binary.Varint(bnums)
		nums--
		log.Printf("Found contents for key '%v'. Shots left: %v", shotKey, nums)
		// Get contents
		contents = datab.Get(bShotKey)
		if contents == nil {
			log.Fatal("a key was found in the nums bucket but not the data bucket")
		}
		// Delete nums and data if this was the last shot. Otherwise decrement nums
		if nums == 0 {
			if err = numsb.Delete(bShotKey); err != nil {
				return err
			}
			return datab.Delete(bShotKey)
		}
		// bnums must be 'remade' to avoid segmentation fault when going from eg. 0 to -1
		// I guess because the buffer used to store 0 is too small for -1 (2's complement?).
		// Size of the buffer is returned by binary.Varint btw, so this is easy to check.
		bnums = make([]byte, binary.MaxVarintLen64)
		binary.PutVarint(bnums, nums)
		numsb.Put(bShotKey, bnums)
		return nil
	})
	if err == nil {
		storageCTRL.bytesUsed -= len(contents)
	}
	return
}

// fixNumShots checks that numshots is valid, i.e. between 1 and max
// (unless an admin header is given), otherwise adjusts to legal values
func legalNumshots(numshots int, r *http.Request) int {
	if r.Header.Get(AdminKeyHeader) == adminKey {
		return numshots
	}
	if numshots < 1 {
		return 1
	}
	if numshots > shotNumsLimit {
		return shotNumsLimit
	}
	return numshots
}

func pour(shotKey string, r *http.Request) (err error) {
	storageCTRL.Lock()
	defer storageCTRL.Unlock()
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
	numshots = legalNumshots(numshots, r)
	if err != nil {
		return err
	}
	if storageCTRL.bytesUsed+len(contents) > storageCTRL.bytesMax {
		return errors.New("database is full")
	}
	fmt.Printf("Number of shots: %v\n", numshots)
	err = db.Update(func(tx *bbolt.Tx) error {
		datab := tx.Bucket([]byte(dataBucketKey))
		numsb := tx.Bucket([]byte(numsBucketKey))
		if datab == nil || numsb == nil {
			log.Fatal("failed to open essential bucket")
		}
		// Put number of shots
		bnums64 := make([]byte, binary.MaxVarintLen64)
		binary.PutVarint(bnums64, int64(numshots))
		err = numsb.Put([]byte(shotKey), bnums64)
		if err != nil {
			return err
		}
		return datab.Put([]byte(shotKey), contents)
	})
	if err == nil {
		storageCTRL.bytesUsed += len(contents)
	}
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

func rootHandler(res http.ResponseWriter, r *http.Request) {
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
			// TODO: Error based on err
			res.Write([]byte("An error occurred"))
			return
		}
		// TODO: Error template. The current handling is the opposite of helpful
		if /*textOnly*/ true {
			response := r.Host + "/" + shotKey
			if _, err := res.Write([]byte(response)); err != nil {
				log.Panicln("Error when trying to write response body")
			}
		}
	}
}

func keyHandler(res http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["shotKey"]
	textOnly := r.Header.Get("Simple") != "" // for forcing textOnly mode
	textOnly = textOnly || strings.Contains(r.Header.Get("User-Agent"), "curl")
	var err error
	if r.Method == http.MethodGet {
		// Return upload page if the key is available
		if !smell(key) {
			writeUploadPage(res, textOnly, key)
			return
		}
		// Otherwise return contents
		contents, err := shot(key)
		if err != nil {
			log.Println(err)
			res.Write(keyTakenMessage)
			res.WriteHeader(http.StatusInternalServerError)
		}
		if _, err = res.Write(contents); err != nil {
			log.Panicln("Error when trying to write response")
			res.WriteHeader(http.StatusInternalServerError)
		}
	} else if r.Method == http.MethodPost {
		// Admin should be able to overwrite anything
		if r.Header.Get(AdminKeyHeader) == adminKey {
			if err = pour(key, r); err != nil {
				goto commonerror
			}
			return
		}
		if smell(key) {
			// POSTs from website to taken shouldn't happen, so use textOnly always
			if _, err = res.Write(keyTakenMessage); err != nil {
				res.WriteHeader(http.StatusInternalServerError)
			}
		} else {
			if err = pour(key, r); err != nil {
				goto commonerror
			}
			res.Write(pourSuccessMessage)
		}
		return
	commonerror:
		// TODO: Notify if database is full, not just server error for everything
		res.WriteHeader(http.StatusInternalServerError)
		res.Write(serverErrorMessage)
		log.Println(err)
	}
	//fmt.Printf("Request from client: %v\n", r.Header.Get("User-Agent"))
	return
}

// Returns summary of the database in the form of 'number of elements, numBytes, err'
// If 'speak' is true, all keys and the size of their corresponding data are printed
func statDB(speak bool) (int, int, error) {
	var numElements, numBytes int
	err := db.View(func(tx *bbolt.Tx) error {
		root := tx.Bucket([]byte(dataBucketKey))
		if root == nil {
			return errors.New("Failed to open root bucket")
		}
		// Not sure if bocket.Stats().LeafInUse equals the actual number of bytes
		// in use, but I think it should be approximately the same
		if !speak {
			numElements = root.Stats().KeyN
			numBytes = root.Stats().LeafInuse
			return nil
		}
		// For 'speak', the bucket must be iterated through anyway, so might as well
		// count the number of elements and bytes manually
		err := root.ForEach(func(k, v []byte) error {
			fmt.Printf("%v %v\n", string(k), len(v))
			numElements++
			numBytes += len(v)
			return nil
		})
		return err
	})
	return numElements, numBytes, err
}

// *init* is a special function, hence the name of this one
// https://tutorialedge.net/golang/the-go-init-function/
func initialize(dbFile string, limitStorage, limitNums, port int, admKey string) error {
	var err error // Because ':=' can't be used on the line below without declaring db as a new *local* variable, making the global one nil
	db, err = bbolt.Open(dbFile, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return err
	}
	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte(dataBucketKey))
		_, err = tx.CreateBucketIfNotExists([]byte(numsBucketKey))
		return err
	})
	if err != nil {
		return err
	}
	_, numBytes, err := statDB(false)
	if err != nil {
		return err
	}
	storageCTRL.bytesMax = 1000 * limitStorage
	storageCTRL.bytesUsed = numBytes
	shotNumsLimit = limitNums
	adminKey = admKey
	fmt.Printf("%v / %v KBs used\n", numBytes/1000, limitStorage)
	fmt.Println("Server started listening at port", port)
	return nil
}

/**************** Main ****************/
func main() {
	port := flag.Int("p", 8080, "Port")
	version := flag.Bool("v", false, "Print version and exit")
	dbFile := flag.String("d", "vodka.db", "Database file")
	stat := flag.Bool("s", false, "View database keys and size of associated contents")
	storageLimit := flag.Int("l", 10000, "Storage limit in kilobytes (1000 bytes)")
	numsLimit := flag.Int("n", 10, "Maximum number of shots per key")
	admKey := flag.String("a", "vodkas", "Admin key to allow unlimited shots")
	flag.Parse()
	if *version {
		fmt.Println("vodkas " + Version)
		return
	}
	err := initialize(*dbFile, *storageLimit, *numsLimit, *port, *admKey)
	defer db.Close()
	if err != nil {
		log.Fatal(err)
	}
	if *stat {
		fmt.Println("Elements in database:")
		numElements, numBytes, err := statDB(true)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\n%v elements in database\n", numElements)
		fmt.Printf("\n%v bytes used\n", numBytes)
		return
	}
	router := mux.NewRouter()
	router.HandleFunc("/", rootHandler)
	router.HandleFunc("/{shotKey}", keyHandler)
	http.Handle("/", router)

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", *port), nil))
}
