package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const IMAGE_ID = 240277

type ErrorResponse struct {
	Error string `json:"error"`
}

type GetIPResponse struct {
	IP string `json:"ip"`
}

type NullResponse struct{}

type LunaVolumeCreate struct {
	VolumeID string `json:"volume_id"`
}

type LunaVolumeInfo struct {
	Volume struct {
		Status string `json:"status"`
	} `json:"volume"`
}

type LunaSshkeyAdd struct {
	KeyID string `json:"key_id"`
}

type LunaScriptCreate struct {
	ScriptID string `json:"script_id"`
}

type LunaNetworkList struct {
	Networks []struct {
		NetID string `json:"net_id"`
	} `json:"networks"`
}

type LunaFloatingList struct {
	IPs []struct {
		IP           string `json:"ip"`
		AttachedType string `json:"attached_type"`
		Region       string `json":region"`
	} `json:"ips"`
}

type LunaVmCreate struct {
	VmID string `json:"vm_id"`
}

type LunaDynList struct {
	Dyns map[string]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		IP   string `json:"ip"`
	} `json:"dyns"`
}

type LunaVmInfo struct {
	Info struct {
		Status string `json:"status_nohtml"`
	} `json:"info"`
}

func main() {
	scriptBytes, err := ioutil.ReadFile("run.sh")
	if err != nil {
		panic(err)
	}
	script := string(scriptBytes)
	fileServer := http.FileServer(http.Dir("static/"))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
	http.HandleFunc("/getip", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		apiID := r.PostForm.Get("api_id")
		apiKey := r.PostForm.Get("api_key")
		ip, err := getFreeFloatingIP(apiID, apiKey, "toronto")
		if err != nil {
			errorResponse(w, r, err.Error())
			return
		}
		if ip == "" {
			err := request(apiID, apiKey, "floating", "add", map[string]string{
				"region": "toronto",
			}, nil)
			if err != nil {
				errorResponse(w, r, err.Error())
				return
			}
			ip, _ = getFreeFloatingIP(apiID, apiKey, "toronto")
			if ip == "" {
				errorResponse(w, r, "failed to get an external IP")
				return
			}
		}
		jsonResponse(w, GetIPResponse{ip})
	})

	http.HandleFunc("/launch", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		apiID := r.PostForm.Get("api_id")
		apiKey := r.PostForm.Get("api_key")
		ip := r.PostForm.Get("ip")
		hostname := r.PostForm.Get("hostname")
		sshKey := r.PostForm.Get("sshkey")
		email := r.PostForm.Get("email")
		network := r.PostForm.Get("network")
		coins := r.PostForm.Get("coins")
		lightning := r.PostForm.Get("lightning")
		repository := r.PostForm.Get("repository")
		branch := r.PostForm.Get("branch")
		plan := r.PostForm.Get("plan")
		accelerate := r.PostForm.Get("accelerate")

		remoteIP := r.RemoteAddr

		myscript := script
		myscript = strings.Replace(myscript, "[HOSTNAME]", hostname, -1)
		myscript = strings.Replace(myscript, "[EMAIL]", email, -1)
		myscript = strings.Replace(myscript, "[NETWORK]", network, -1)
		myscript = strings.Replace(myscript, "[COINS]", coins, -1)
		myscript = strings.Replace(myscript, "[LIGHTNING]", lightning, -1)
		myscript = strings.Replace(myscript, "[REPOSITORY]", repository, -1)
		myscript = strings.Replace(myscript, "[BRANCH]", branch, -1)

		var cleanupFuncs []func()
		cleanup := func() {
			for _, f := range cleanupFuncs {
				f()
			}
		}

		// create DNS if desired
		if strings.HasSuffix(hostname, ".lndyn.com") && strings.HasPrefix(hostname, "bitcartcc") && len(hostname) == 25 {
			baseName := strings.Split(hostname, ".")[0]
			err := request(apiID, apiKey, "dns", "dyn-add", map[string]string{
				"name": baseName,
				"ip":   ip,
			}, nil)
			if err != nil {
				cleanup()
				errorResponse(w, r, err.Error())
				return
			}
			cleanupFuncs = append(cleanupFuncs, func() {
				var response LunaDynList
				if err := request(apiID, apiKey, "dns", "dyn-list", nil, &response); err != nil {
					log.Printf("warning: error listing dyns for cleanup: %v", err)
					return
				}
				for _, dyn := range response.Dyns {
					if dyn.Name == baseName || dyn.IP == ip {
						request(apiID, apiKey, "dns", "dyn-remove", map[string]string{"dyn_id": dyn.ID}, nil)
					}
				}
			})
		}

		params := map[string]string{
			"region":   "toronto",
			"plan_id":  plan,
			"image_id": strconv.Itoa(IMAGE_ID),
			"ip":       ip,
			"hostname": hostname,
		}

		log.Printf("[%s] creating vm", remoteIP)

		// add ssh key, if set
		if sshKey != "" {
			var keyResponse LunaSshkeyAdd
			err := request(apiID, apiKey, "sshkey", "add", map[string]string{
				"label":  "tmp",
				"sshkey": sshKey,
			}, &keyResponse)
			if err != nil {
				cleanup()
				errorResponse(w, r, "error adding SSH key: "+err.Error())
				return
			}
			params["key_id"] = keyResponse.KeyID
			defer func() {
				request(apiID, apiKey, "sshkey", "remove", map[string]string{
					"key_id": keyResponse.KeyID,
				}, nil)
			}()
		} else {
			params["set_password"] = "yes"
		}

		// add startup script
		var scriptResponse LunaScriptCreate
		err = request(apiID, apiKey, "script", "create", map[string]string{
			"name":    "tmp-bitcartcc",
			"content": myscript,
		}, &scriptResponse)
		if err != nil {
			cleanup()
			errorResponse(w, r, "error creating startup script: "+err.Error())
			return
		}
		params["scripts"] = scriptResponse.ScriptID
		defer func() {
			request(apiID, apiKey, "script", "delete", map[string]string{
				"script_id": scriptResponse.ScriptID,
			}, nil)
		}()

		// should we set network?
		var networkResponse LunaNetworkList
		request(apiID, apiKey, "network", "list", map[string]string{
			"region": "toronto",
		}, &networkResponse)
		if len(networkResponse.Networks) >= 1 {
			params["net_id"] = networkResponse.Networks[0].NetID
		}

		// create vm
		var vmResponse LunaVmCreate
		err = request(apiID, apiKey, "vm", "create", params, &vmResponse)
		if err != nil {
			cleanup()
			errorResponse(w, r, "error creating VM: "+err.Error())
			return
		}
		done := false
		for i := 0; i < 10; i++ {
			time.Sleep(5 * time.Second)
			var infoResponse LunaVmInfo
			err := request(apiID, apiKey, "vm", "info", map[string]string{
				"vm_id": vmResponse.VmID,
			}, &infoResponse)
			if err != nil {
				cleanup()
				errorResponse(w, r, "error waiting for VM: "+err.Error())
				return
			}
			if infoResponse.Info.Status == "Online" {
				done = true
				break
			}
		}
		if !done {
			cleanup()
			errorResponse(w, r, "timed out waiting for VM creation")
			return
		}

		// enable charge_for_cpu if desired
		if accelerate == "yes" {
			request(apiID, apiKey, "vm", "set-fairshare", map[string]string{
				"vm_id":            vmResponse.VmID,
				"charge_for_cpu":   "yes",
				"fairshare_nolend": "no",
			}, nil)
		}

		jsonResponse(w, NullResponse{})
	})
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func errorResponse(w http.ResponseWriter, r *http.Request, msg string) {
	log.Printf("[%s] error: %s", r.RemoteAddr, msg)
	jsonResponse(w, ErrorResponse{msg})
}

func jsonResponse(w http.ResponseWriter, x interface{}) {
	bytes, err := json.Marshal(x)
	if err != nil {
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(bytes)
}

const LNDYNAMIC_API_URL = "https://dynamic.lunanode.com/api/{CATEGORY}/{ACTION}/"

func request(apiID string, apiKey string, category string, action string, params map[string]string, target interface{}) error {
	if len(apiID) != 16 {
		return fmt.Errorf("API ID should be 16 characters")
	} else if len(apiKey) != 128 {
		return fmt.Errorf("API key should be 128 characters")
	}

	// construct URL
	targetUrl := LNDYNAMIC_API_URL
	targetUrl = strings.Replace(targetUrl, "{CATEGORY}", category, -1)
	targetUrl = strings.Replace(targetUrl, "{ACTION}", action, -1)

	// get raw parameters string
	if params == nil {
		params = make(map[string]string)
	}
	params["api_id"] = apiID
	params["api_partialkey"] = apiKey[:64]
	rawParams, err := json.Marshal(params)
	if err != nil {
		return err
	}

	// HMAC signature with nonce
	nonce := fmt.Sprintf("%d", time.Now().Unix())
	handler := fmt.Sprintf("%s/%s/", category, action)
	hashTarget := fmt.Sprintf("%s|%s|%s", handler, string(rawParams), nonce)
	hasher := hmac.New(sha512.New, []byte(apiKey))
	_, err = hasher.Write([]byte(hashTarget))
	if err != nil {
		return err
	}
	signature := hex.EncodeToString(hasher.Sum(nil))

	// perform request
	values := url.Values{}
	values.Set("handler", handler)
	values.Set("req", string(rawParams))
	values.Set("signature", signature)
	values.Set("nonce", nonce)
	byteBuffer := new(bytes.Buffer)
	byteBuffer.Write([]byte(values.Encode()))
	response, err := http.Post(targetUrl, "application/x-www-form-urlencoded", byteBuffer)
	if err != nil {
		return err
	}
	responseBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	response.Body.Close()

	// decode JSON
	// we first decode into generic response for error checking; then into specific response to return
	var genericResponse struct {
		Success string `json:"success"`
		Error   string `json:"error"`
	}

	err = json.Unmarshal(responseBytes, &genericResponse)
	if err != nil {
		return err
	} else if genericResponse.Success != "yes" {
		if genericResponse.Error != "" {
			return fmt.Errorf(genericResponse.Error)
		} else {
			return fmt.Errorf("backend call failed for unknown reason")
		}
	}

	if target != nil {
		err = json.Unmarshal(responseBytes, target)
		if err != nil {
			return err
		}
	}

	return nil
}

func getFreeFloatingIP(apiID string, apiKey string, region string) (string, error) {
	var response LunaFloatingList
	if err := request(apiID, apiKey, "floating", "list", nil, &response); err != nil {
		return "", err
	}
	for _, ip := range response.IPs {
		if ip.AttachedType == "unattached" && ip.Region == region {
			return ip.IP, nil
		}
	}
	return "", nil
}

func createVolume(apiID string, apiKey string, hostname string, size string) (string, error, func()) {
	var createResponse LunaVolumeCreate
	err := request(apiID, apiKey, "volume", "create", map[string]string{
		"region": "toronto",
		"label":  fmt.Sprintf("%s", hostname),
		"size":   size,
	}, &createResponse)
	if err != nil {
		return "", err, nil
	}
	cleanup := func() {
		request(apiID, apiKey, "volume", "delete", map[string]string{
			"volume_id": createResponse.VolumeID,
		}, nil)
	}

	done := false
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		var infoResponse LunaVolumeInfo
		err := request(apiID, apiKey, "volume", "info", map[string]string{
			"volume_id": createResponse.VolumeID,
		}, &infoResponse)
		if err != nil {
			cleanup()
			return "", err, nil
		}
		if infoResponse.Volume.Status == "available" {
			done = true
			break
		}
	}
	if !done {
		cleanup()
		return "", fmt.Errorf("timed out waiting for volume creation"), nil
	}
	return createResponse.VolumeID, nil, cleanup
}
