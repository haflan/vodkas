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
        "strings"
        "strconv"
        "time"
)


/**************** Handler Functions ****************/

const FormNameFile = "file"
const FormNameText = "text"
const FormNameNumShots = "numdls"

const InfoMessage = `Usage: 
- POUR:     curl vetle.vodka[/<requested-shot-key>] -d <data>
- SHOT:     curl vetle.vodka/<shot-key>

A shot key is an at-the-moment unique ID that's linked to the dumped data.
As soon as a specific shot has been accessed both the link and the contents 
are removed completely.
`

var rootBucket = "root"
var db *bbolt.DB

// Pushes new data to database. Might be replaced by pour()
func push(key string, contents []byte) {
        err := db.Update(func(tx *bbolt.Tx) error {
                b := tx.Bucket([]byte(rootBucket))
                if b == nil {
                      return errors.New("Failed to open root bucket")
                }
                err := b.Put([]byte(key), contents)
                return err
        })
        if err != nil {
                log.Printf("Push to '%v' failed: %v", key, err)
        }
}
// Pops data from database. Will probably be replaced by shot(), and support more
// than a single download (although that will still be the default)
func pop(key string) (contents []byte, found bool) {
        err := db.Update(func(tx *bbolt.Tx) error {
                b := tx.Bucket([]byte(rootBucket))
                if b == nil {
                        return errors.New("Failed to open root bucket")
                }
                contents = b.Get([]byte(key))
                found = contents != nil
                if found {
                        fmt.Printf("Found contents for shotkey %v\n", key)
                        return b.Delete([]byte(key))
                }
                return nil
        })
        if err != nil {
                log.Printf("Push to '%v' failed: %v", key, err)
        }
        return
}

func pour(key string, formdata []byte) {
        fmt.Println(key)
        fmt.Println(string(formdata))
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
            if err != nil { return }
            num, err = strconv.Atoi(string(numShotsRaw))
            if err != nil { return }
        }
    }
    err = nil
    return
}


func RootHandler(res http.ResponseWriter, r *http.Request) {
        // Detect whether Simple mode (text only) is active
        textOnly := r.Header.Get("Simple") != "" // for forcing textOnly mode
        textOnly = textOnly || strings.Contains(r.Header.Get("User-Agent"), "curl")
        var response string
        if r.Method == http.MethodGet {
                if textOnly {
                        response = InfoMessage
                        if _, err := res.Write([]byte(response)); err != nil {
                                log.Panicln("Error when trying to write response body")
                        }
                } else {
                        templateData := struct { ShotKey string }{ "" }
                        uploadPageTemplate.Execute(res, templateData)
                }
                return
        } else if r.Method == http.MethodPost {
            var err error
            var numshots int
            var contents []byte
            // Dumps can be both x-www-urlencoded and multipart/form-data. 
            // Try multipart first, then x-www-urlencoded
            mpReader, _ := r.MultipartReader()
            if mpReader != nil {
                contents, numshots, err = extractMultipart(mpReader)
            } else {
                numshots = 1
                contents, err = ioutil.ReadAll(r.Body)
            }
            if err != nil {
                    res.WriteHeader(http.StatusInternalServerError)
                    return
            }
            fmt.Printf("Number of shots: %v\n", numshots)
            fmt.Printf("Bytes in contents: %v\n", len(contents))
            // Generate random shot key
            random := make([]byte, 16)
            rand.Read(random)
            randhex := hex.EncodeToString(random)
            push(randhex, contents)
            if /*textOnly*/ true {
                    response = r.Host + "/" + randhex
                    if _, err := res.Write([]byte(response)); err != nil {
                            log.Panicln("Error when trying to write response body")
                    }
            }
            return
        }
}

func KeyHandler(res http.ResponseWriter, r *http.Request) {
        key := mux.Vars(r)["shotKey"]
        contents, found := pop(key)
        // GET requests only
        /*if !found {
                res.WriteHeader(http.StatusNotFound)
                if _, err := res.Write([]byte(fmt.Sprint("404 no shot here\n"))); err != nil {
                        log.Panicln("Error when trying to write response")
                }
                return*/
        if found {
                // For POST requests to specific key, this should actually return 'link taken' or something
                if _, err := res.Write([]byte(contents)); err != nil {
                        log.Panicln("Error when trying to write response body")
                }
        } else {
                b, err := ioutil.ReadAll(r.Body)
                if err != nil {
                        res.WriteHeader(http.StatusInternalServerError)
                        log.Panicln("Error when trying to read body")
                }
                push(key, b)
                if _, err := res.Write([]byte(fmt.Sprint("Contents stored in given link\n"))); err != nil {
                        log.Panicln("Error when trying to write response")
                }
        }
        fmt.Printf("Request from client: %v\n", r.Header.Get("User-Agent"))
        return
}

/**************** Main ****************/
func main(){
        port := flag.Int("p", 8080, "Port")
        dbFile := flag.String("d", "vodka.db", "Database file")
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

        defer fmt.Println("Server shutting down")
        fmt.Println("Server started listening at port", *port)
        router := mux.NewRouter()
        router.HandleFunc("/", RootHandler)
        router.HandleFunc("/{shotKey}", KeyHandler)
        http.Handle("/", router)

        log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", *port), nil))
}
