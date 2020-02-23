//  Seems to work: `go build -ldflags "-linkmode external -extldflags -static"`
package main

import (
        //"crypto/rand"
        "flag"
        "fmt"
        "github.com/gorilla/mux"
        "github.com/pkg/errors"
        "go.etcd.io/bbolt"
        "io/ioutil"
        "log"
        "net/http"
        "strings"
        "time"
)


/**************** Handler Functions ****************/

const InfoMessage = `Usage: 
- POUR:     curl vetle.vodka[/<requested-shot-code>] -d <data>
- SHOT:     curl vetle.vodka/<shot-code>

A shot code is an at-the-moment unique ID that's linked to the dumped data.
As soon as a specific shot has been accessed both the link and the contents 
are removed completely.
`

var rootBucket = "root"
var db *bbolt.DB

func push(code string, contents []byte) {
        err := db.Update(func(tx *bbolt.Tx) error {
                b := tx.Bucket([]byte(rootBucket))
                if b == nil {
                      return errors.New("Failed to open root bucket")
                }
                err := b.Put([]byte(code), contents)
                return err
        })
        if err != nil {
                log.Printf("Push to '%v' failed: %v", code, err)
        }
}

func pop(code string) (contents []byte, found bool) {
        err := db.Update(func(tx *bbolt.Tx) error {
                b := tx.Bucket([]byte(rootBucket))
                if b == nil {
                        return errors.New("Failed to open root bucket")
                }
                contents = b.Get([]byte(code))
                found = contents != nil
                if found {
                        fmt.Printf("Found contents for shotcode %v\n", code)
                        return b.Delete([]byte(code))
                }
                return nil
        })
        if err != nil {
                log.Printf("Push to '%v' failed: %v", code, err)
        }
        return
}

// TODO: Separate GETs and POSTs
func MainHandler(res http.ResponseWriter, r *http.Request) {
        b, err := ioutil.ReadAll(r.Body)
        if err != nil {
                res.WriteHeader(http.StatusInternalServerError)
                return
        }
        push("sorandom", b)
        textOnly := r.Header.Get("Simple") != ""    // Should be able to force simple
        textOnly = textOnly || strings.Contains(r.Header.Get("User-Agent"), "curl")
        var responseText string
        if textOnly {
                responseText = InfoMessage
        } else {
                responseText = "This will be replaced by a template"
        }
        if _, err = res.Write([]byte(responseText)); err != nil {
                log.Panicln("Error when trying to write response body")
        }
        return
}

// func PourHandler(res http.ResponseWriter, r *http.Request) {
//}

// TODO: Generate random code if no request given (https://flaviocopes.com/go-random/)
func ShotHandler(res http.ResponseWriter, r *http.Request) {
        code := mux.Vars(r)["shotCode"]
        contents, found := pop(code)
        // GET requests only
        /*if !found {
                res.WriteHeader(http.StatusNotFound)
                if _, err := res.Write([]byte(fmt.Sprint("404 no shot here\n"))); err != nil {
                        log.Panicln("Error when trying to write response")
                }
                return*/
        if found {
                // For POST requests to specific code, this should actually return 'link taken' or something
                if _, err := res.Write([]byte(contents)); err != nil {
                        log.Panicln("Error when trying to write response body")
                }
        } else {
                b, err := ioutil.ReadAll(r.Body)
                if err != nil {
                        res.WriteHeader(http.StatusInternalServerError)
                        log.Panicln("Error when trying to read body")
                }
                push(code, b)
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
        router.HandleFunc("/", MainHandler)
        router.HandleFunc("/{shotCode}", ShotHandler)
        http.Handle("/", router)

        log.Fatal(http.ListenAndServe(fmt.Sprintf(":%v", *port), nil))
}
