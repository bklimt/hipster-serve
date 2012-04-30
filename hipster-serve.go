// Author: Ryan L Brown
// License: Use how you wish with attribution.

package main

import (
	"flag"
	"html/template"
	"io/ioutil"
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const cmdsFileName = "hipster-serve.json"

var (
	port = flag.Int("port", 8080, "port to listen on")
	templ = template.Must(template.New("config").Parse(configHtml))
	cmds = NewCache()
	cache = NewCache()
)

// A concurrent map from string to interface{}.
type Cache struct {
	Data map[string]interface{}
	Lock sync.RWMutex
}

func NewCache() *Cache {
	return &Cache{ Data: make(map[string]interface{}) }
}

func (c *Cache) get(key string) (value interface{}, ok bool) {
	c.Lock.RLock()
	defer c.Lock.RUnlock()
	value, ok = c.Data[key]
	return
}

func (c *Cache) GetBytes(key string) (value []byte, ok bool) {
	v, ok := c.get(key)
	if ok {
		value = v.([]byte)
	}
	return
}

func (c *Cache) Put(key string, value interface{}) {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	c.Data[key] = value
}

func (c *Cache) Delete(key string) {
	c.Lock.Lock()
	defer c.Lock.Unlock()
	delete(c.Data, key)
}

// Returns the cmd registered to a suffix of |path|. There will only
// be one such suffix because one suffix can't be a suffix of another.
func getCmd(path string) (cmd string, ok bool) {
	cmds.Lock.RLock()
	defer cmds.Lock.RUnlock()
	for k, v := range cmds.Data {
		if strings.HasSuffix(path, k) {
			return "cat " + path + " | " + v.(string), true
		}
	}
	return "", false
}

// Returns false if |suffix| is a suffix of another suffix or vice
// versa. Assumes a read or write lock is held on |cmds|.
func valid(suffix string) bool {
	for k, _ := range cmds.Data {
		if strings.HasSuffix(suffix, k) || strings.HasSuffix(k, suffix) {
			return false
		}
	}
	return true
}

// Serves the config page at localhost:port/.
func configHandler(w http.ResponseWriter, r *http.Request) {
	// Get any POST data.
	r.ParseForm()
	rm := strings.TrimSpace(r.FormValue("rm"))
	suffix := strings.TrimSpace(r.FormValue("suffix"))
	cmd := strings.TrimSpace(r.FormValue("cmd"))
	// Make the changes requested.
	error := ""
	if rm != "" && suffix != "" && cmd != "" {
		cmds.Lock.Lock()
		log.Printf("inside lock")
		changed := false
		if rm == "true" {
			delete(cmds.Data, suffix)
			changed = true
			log.Printf("Deleted cmd for suffix: %s", suffix)
		} else if valid(suffix) {
			cmds.Data[suffix] = cmd
			changed = true
			log.Printf("Saved cmd: %s | %s", suffix, cmd)
		} else {
			error = "One suffix can't be a suffix of another."
		}
		if changed {
			// Save the cmds to file.
			f, err := os.Create(cmdsFileName)
			if err != nil {
				log.Printf("Not able to create files!")
			} else {
				err = json.NewEncoder(f).Encode(&cmds.Data)
				f.Close()
				if err != nil {
					log.Printf("Not able to write json to file!")
				}
			}
		}
		cmds.Lock.Unlock()
	}
	// Make a copy of cmds so we have a stable version.
	m := make(map[string]string)
	cmds.Lock.RLock()
	for k, v := range cmds.Data {
		m[k] = v.(string)
	}
	cmds.Lock.RUnlock()
	// Write the template.
	templ.Execute(w, map[string]interface{}{"Cmds": m, "HasError": error != "", "Error": error})
}

// Serves any files from localhost:port/file/path optionally running
// any commands that are registered to a suffix of the path or perhaps
// reading from the cache.
func fileHandler(w http.ResponseWriter, r *http.Request) {
	useCache := (strings.ToLower(r.URL.Query().Get("cache")) == "yes")
	path := "." + r.URL.Path
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(path)))
	if useCache {
		bytes, ok := cache.GetBytes(path)
		if ok {
			w.Write(bytes)
			log.Printf("Read from cache for %s", path)
			return
		}
	} else {
		cache.Delete(path)
	}
	cmd, ok := getCmd(path)
	bytes := []byte{}
	err := error(nil)
	if ok {
		bytes, err = exec.Command("bash", "-c", cmd).Output()
		log.Printf("Ran cmd: %s", cmd)
	} else {
		bytes, err = ioutil.ReadFile(path)
		log.Printf("Read file: %s", path)
	}
	if err != nil {
		bytes = []byte(err.Error())
	} else if useCache {
		cache.Put(path, bytes)
		log.Printf("Cached data for %s", path)
	}
	w.Write(bytes)
}

// Handles all requests.
func handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/": configHandler(w, r)
	default: fileHandler(w, r)
	}
}

func main() {
	flag.Parse()
	// Load |cmds| from the json file if it's there.
	f, err := os.Open(cmdsFileName)
	if err == nil {
		err = json.NewDecoder(f).Decode(&cmds.Data)
		f.Close()
		if err != nil {
			panic("Can't read hipster-serve.json.")
		}
	}
	// Start serving.
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":" + strconv.Itoa(*port), nil))
}

// The config page at localhost:port/.
const configHtml = `
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>build-serve config</title>
    <style>
      html, body, p, ul, li { margin:0; padding:0 }
      body { font-family:monospace; padding:20px; }
      ul { list-style-type:none }
      li { padding: 7px 0; }
      .command-box { width:60%; }
      .suffix-box { width:50px }
      .suffix { font-weight:bold; }
      .error { color:red; font-size:16px; font-family:sans-serif; padding:7px 0; }
      #container { margin:0 auto; width:800px; }
    </style>
  </head>
  <body>
    <div id="container">
      <h1>build-serve config</h1>
      {{if .HasError}}
        <p class="error">{{.Error}}</p>
      {{end}}
      <ul>
        {{ range $i, $v := .Cmds }}
          <li>
            <form method="post" enctype="application/x-www-form-urlencoded" action="/">
              <input type="hidden" name="suffix" value="{{$i}}" />
              <input type="hidden" name="cmd" value="{{$v}}" />
              <input type="hidden" name="rm" value="true" />
              <button>remove</button>
              <span class="suffix">{{$i}}</span> | {{$v}}
            </form>
          </li>
        {{end}}
        <li>
          <h3>New rule:</h3>
          <form method="post" enctype="application/x-www-form-urlencoded" action="/">
            <label>suffix:<input class="suffix-box" name="suffix" /></label>
            <label>cmd:<input class="command-box" name="cmd" /></label>
            <input type="hidden" name="rm" value="false" />
            <button>save</button>
          </form>
        </li>
      </ul>
    </div>
  </body>
</html>
`
