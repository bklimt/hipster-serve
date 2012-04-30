// Author: Ryan L Brown
// License: Use how you wish with attribution.

package main

import (
	"html/template"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	templ = template.Must(template.New("config").Parse(configHtml))
	cmds = NewCache()
	cache = NewCache()
)

// A concurrent map from string to []byte.
type Cache struct {
	Data map[string][]byte
	Lock sync.RWMutex
}

func NewCache() *Cache {
	return &Cache{ Data: make(map[string][]byte) }
}

func (c *Cache) Get(key string) (value []byte, ok bool) {
	c.Lock.RLock()
	defer c.Lock.RUnlock()
	value, ok = c.Data[key]
	return
}

func (c *Cache) Put(key string, value []byte) {
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
			return "cat " + path + " | " + string(v), true
		}
	}
	return "", false
}

// Returns false if |suffix| is a suffix of another suffix or vice
// versa.
func valid(suffix string) bool {
	cmds.Lock.RLock()
	defer cmds.Lock.RUnlock()
	for k, _ := range cmds.Data {
		if strings.HasSuffix(suffix, k) || strings.HasSuffix(k, suffix) {
			return false
		}
	}
	return true
}

// Serves the config page at localhost:port/.
func configHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	rm := strings.TrimSpace(r.FormValue("rm"))
	suffix := strings.TrimSpace(r.FormValue("suffix"))
	cmd := strings.TrimSpace(r.FormValue("cmd"))
	error := ""
	if rm != "" && suffix != "" && cmd != "" {
		if rm == "true" {
			cmds.Delete(suffix)
			log.Printf("Deleted cmd for suffix: %s", suffix)
		} else if valid(suffix) {
			cmds.Put(suffix, []byte(cmd))
			log.Printf("Saved cmd: %s | %s", suffix, cmd)
		} else {
			error = "One suffix can't be a suffix of another."
		}
	}
	m := make(map[string]string)
	cmds.Lock.RLock()
	for k, v := range cmds.Data {
		m[k] = string(v)
	}
	cmds.Lock.RUnlock()
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
		bytes, ok := cache.Get(path)
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
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":8080", nil))
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
