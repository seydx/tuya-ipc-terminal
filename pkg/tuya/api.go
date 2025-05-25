package tuya

import (
	"bytes"
	"crypto/md5"
	cryptoRand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type QRCodeResponse struct {
	Result  string `json:"result"`
	T       int64  `json:"t"`
	Success bool   `json:"success"`
	Msg     string `json:"errorMsg,omitempty"`
}

type PollResponse struct {
	Result  interface{} `json:"result"`
	T       int64       `json:"t"`
	Success bool        `json:"success"`
	Msg     string      `json:"errorMsg,omitempty"`
}

type LoginTokenRequest struct {
	CountryCode string `json:"countryCode"`
	Username    string `json:"username"`
	IsUid       bool   `json:"isUid"`
}

type LoginTokenResponse struct {
	Result struct {
		Token     string `json:"token"`
		Exponent  string `json:"exponent"`
		PublicKey string `json:"publicKey"`
		PbKey     string `json:"pbKey"`
	} `json:"result"`
	Success bool   `json:"success"`
	Msg     string `json:"errorMsg,omitempty"`
}

type PasswordLoginRequest struct {
	CountryCode string `json:"countryCode"`
	Email       string `json:"email,omitempty"`
	Mobile      string `json:"mobile,omitempty"`
	Passwd      string `json:"passwd"`
	Token       string `json:"token"`
	IfEncrypt   int    `json:"ifencrypt"`
	Options     string `json:"options"`
}

type PasswordLoginResponse struct {
	Result   LoginResult `json:"result"`
	Success  bool        `json:"success"`
	Status   string      `json:"status"`
	ErrorMsg string      `json:"errorMsg,omitempty"`
}

type LoginResult struct {
	Attribute          int    `json:"attribute"`
	ClientId           string `json:"clientId"`
	DataVersion        int    `json:"dataVersion"`
	Domain             Domain `json:"domain"`
	Ecode              string `json:"ecode"`
	Email              string `json:"email"`
	Extras             Extras `json:"extras"`
	HeadPic            string `json:"headPic"`
	ImproveCompanyInfo bool   `json:"improveCompanyInfo"`
	Nickname           string `json:"nickname"`
	PartnerIdentity    string `json:"partnerIdentity"`
	PhoneCode          string `json:"phoneCode"`
	Receiver           string `json:"receiver"`
	RegFrom            int    `json:"regFrom"`
	Sid                string `json:"sid"`
	SnsNickname        string `json:"snsNickname"`
	TempUnit           int    `json:"tempUnit"`
	Timezone           string `json:"timezone"`
	TimezoneId         string `json:"timezoneId"`
	Uid                string `json:"uid"`
	UserType           int    `json:"userType"`
	Username           string `json:"username"`
}

type Domain struct {
	AispeechHttpsUrl    string `json:"aispeechHttpsUrl"`
	AispeechQuicUrl     string `json:"aispeechQuicUrl"`
	DeviceHttpUrl       string `json:"deviceHttpUrl"`
	DeviceHttpsPskUrl   string `json:"deviceHttpsPskUrl"`
	DeviceHttpsUrl      string `json:"deviceHttpsUrl"`
	DeviceMediaMqttUrl  string `json:"deviceMediaMqttUrl"`
	DeviceMediaMqttsUrl string `json:"deviceMediaMqttsUrl"`
	DeviceMqttsPskUrl   string `json:"deviceMqttsPskUrl"`
	DeviceMqttsUrl      string `json:"deviceMqttsUrl"`
	GwApiUrl            string `json:"gwApiUrl"`
	GwMqttUrl           string `json:"gwMqttUrl"`
	HttpPort            int    `json:"httpPort"`
	HttpsPort           int    `json:"httpsPort"`
	HttpsPskPort        int    `json:"httpsPskPort"`
	MobileApiUrl        string `json:"mobileApiUrl"`
	MobileMediaMqttUrl  string `json:"mobileMediaMqttUrl"`
	MobileMqttUrl       string `json:"mobileMqttUrl"`
	MobileMqttsUrl      string `json:"mobileMqttsUrl"`
	MobileQuicUrl       string `json:"mobileQuicUrl"`
	MqttPort            int    `json:"mqttPort"`
	MqttQuicUrl         string `json:"mqttQuicUrl"`
	MqttsPort           int    `json:"mqttsPort"`
	MqttsPskPort        int    `json:"mqttsPskPort"`
	RegionCode          string `json:"regionCode"`
}

type Extras struct {
	HomeId    string `json:"homeId"`
	SceneType string `json:"sceneType"`
}

type AppInfo struct {
	AppId    int    `json:"appId"`
	AppName  string `json:"appName"`
	ClientId string `json:"clientId"`
	Icon     string `json:"icon"`
}

type AppInfoResponse struct {
	Result  AppInfo `json:"result"`
	T       int64   `json:"t"`
	Success bool    `json:"success"`
	Msg     string  `json:"errorMsg,omitempty"`
}

type MQTConfig struct {
	Msid     string `json:"msid"`
	Password string `json:"password"`
}

type MQTTConfigResponse struct {
	Result  MQTConfig `json:"result"`
	Success bool      `json:"success"`
	Msg     string    `json:"errorMsg,omitempty"`
}

type HomeListResponse struct {
	Result  []Home `json:"result"`
	T       int64  `json:"t"`
	Success bool   `json:"success"`
	Msg     string `json:"errorMsg,omitempty"`
}

type SharedHomeListResponse struct {
	Result  SharedHome `json:"result"`
	T       int64      `json:"t"`
	Success bool       `json:"success"`
	Msg     string     `json:"errorMsg,omitempty"`
}

type SharedHome struct {
	SecurityWebCShareInfoList []struct {
		DeviceInfoList []Device `json:"deviceInfoList"`
		Nickname       string   `json:"nickname"`
		Username       string   `json:"username"`
	} `json:"securityWebCShareInfoList"`
}

type Home struct {
	Admin            bool    `json:"admin"`
	Background       string  `json:"background"`
	DealStatus       int     `json:"dealStatus"`
	DisplayOrder     int     `json:"displayOrder"`
	GeoName          string  `json:"geoName"`
	Gid              int     `json:"gid"`
	GmtCreate        int64   `json:"gmtCreate"`
	GmtModified      int64   `json:"gmtModified"`
	GroupId          int     `json:"groupId"`
	GroupUserId      int     `json:"groupUserId"`
	Id               int     `json:"id"`
	Lat              float64 `json:"lat"`
	Lon              float64 `json:"lon"`
	ManagementStatus bool    `json:"managementStatus"`
	Name             string  `json:"name"`
	OwnerId          string  `json:"ownerId"`
	Role             int     `json:"role"`
	Status           bool    `json:"status"`
	Uid              string  `json:"uid"`
}

type RoomListResponse struct {
	Result  []Room `json:"result"`
	T       int64  `json:"t"`
	Success bool   `json:"success"`
	Msg     string `json:"errorMsg,omitempty"`
}

type Room struct {
	DeviceCount int      `json:"deviceCount"`
	DeviceList  []Device `json:"deviceList"`
	RoomId      string   `json:"roomId"`
	RoomName    string   `json:"roomName"`
}

type Device struct {
	Category            string `json:"category"`
	DeviceId            string `json:"deviceId"`
	DeviceName          string `json:"deviceName"`
	P2pType             int    `json:"p2pType"`
	ProductId           string `json:"productId"`
	SupportCloudStorage bool   `json:"supportCloudStorage"`
	Uuid                string `json:"uuid"`
}

type WebRTCConfigResponse struct {
	Result  WebRTCConfig `json:"result"`
	Success bool         `json:"success"`
	Msg     string       `json:"errorMsg,omitempty"`
}

type WebRTCConfig struct {
	AudioAttributes     AudioAttributes `json:"audioAttributes"`
	Auth                string          `json:"auth"`
	GatewayId           string          `json:"gatewayId"`
	Id                  string          `json:"id"`
	LocalKey            string          `json:"localKey"`
	MotoId              string          `json:"motoId"`
	NodeId              string          `json:"nodeId"`
	P2PConfig           P2PConfig       `json:"p2pConfig"`
	ProtocolVersion     string          `json:"protocolVersion"`
	Skill               string          `json:"skill"`
	Sub                 bool            `json:"sub"`
	SupportWebrtcRecord bool            `json:"supportWebrtcRecord"`
	SupportsPtz         bool            `json:"supportsPtz"`
	SupportsWebrtc      bool            `json:"supportsWebrtc"`
	VedioClarity        int             `json:"vedioClarity"`
	VedioClaritys       []int           `json:"vedioClaritys"`
	VideoClarity        int             `json:"videoClarity"`
}

type AudioSkill struct {
	Channels   int `json:"channels"`
	DataBit    int `json:"dataBit"`
	CodecType  int `json:"codecType"`
	SampleRate int `json:"sampleRate"`
}

type VideoSkill struct {
	StreamType int    `json:"streamType"` // 2 = main stream (hd), 4 = sub stream (sd)
	ProfileId  string `json:"profileId,omitempty"`
	CodecType  int    `json:"codecType"` // 2 = H264, 4 = H265
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SampleRate int    `json:"sampleRate"`
}

type Skill struct {
	WebRTC int          `json:"webrtc"`
	Audios []AudioSkill `json:"audios"`
	Videos []VideoSkill `json:"videos"`
}

type AudioAttributes struct {
	CallMode           []int `json:"callMode"`
	HardwareCapability []int `json:"hardwareCapability"`
}

type P2PConfig struct {
	Auth   string      `json:"auth"`
	Ices   []ICEServer `json:"ices"`
	MotoId string      `json:"motoId"`
}

type ICEServer struct {
	Urls       string `json:"urls"`
	Credential string `json:"credential,omitempty"`
	Username   string `json:"username,omitempty"`
	Ttl        int    `json:"ttl,omitempty"`
}

type DataChannelMessage struct {
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

type RecvMessage struct {
	Video struct {
		SSRC uint32 `json:"ssrc"`
	} `json:"video"`
	Audio struct {
		SSRC uint32 `json:"ssrc"`
	} `json:"audio"`
}

func PasswordLogin(client *http.Client, serverHost, email, password, countryCode string) (*LoginResult, error) {
	// Step 1: Get login token
	tokenResp, err := GetLoginToken(client, serverHost, email, countryCode)
	if err != nil {
		return nil, err
	}

	// Step 2: Encrypt password with RSA
	encryptedPassword, err := encryptPassword(password, tokenResp.Result.PbKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt password: %v", err)
	}

	// Step 3: Perform login
	var loginResp *PasswordLoginResponse
	var url string

	loginReq := PasswordLoginRequest{
		CountryCode: countryCode,
		Passwd:      encryptedPassword,
		Token:       tokenResp.Result.Token,
		IfEncrypt:   1,
		Options:     `{"group":1}`,
	}

	if isEmailAddress(email) {
		url = fmt.Sprintf("https://%s/api/private/email/login", serverHost)
		loginReq.Email = email
	} else {
		url = fmt.Sprintf("https://%s/api/private/phone/login", serverHost)
		loginReq.Mobile = email
	}

	loginResp, err = performLogin(client, url, loginReq, serverHost)

	if err != nil {
		return nil, err
	}

	if !loginResp.Success {
		return nil, errors.New(loginResp.ErrorMsg)
	}

	return &loginResp.Result, nil
}

func GetLoginToken(client *http.Client, serverHost, username, countryCode string) (*LoginTokenResponse, error) {
	url := fmt.Sprintf("https://%s/api/login/token", serverHost)

	tokenReq := LoginTokenRequest{
		CountryCode: countryCode,
		Username:    username,
		IsUid:       false,
	}

	jsonData, err := json.Marshal(tokenReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/login", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp LoginTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}

	if !tokenResp.Success {
		return nil, err
	}

	return &tokenResp, nil
}

func GenerateQRCode(client *http.Client, serverHost string) (string, error) {
	url := fmt.Sprintf("https://%s/api/login/security/QCtoken", serverHost)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/login", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var qrResponse QRCodeResponse
	if err := json.Unmarshal(body, &qrResponse); err != nil {
		return "", err
	}

	if !qrResponse.Success {
		return "", errors.New(qrResponse.Msg)
	}

	return qrResponse.Result, nil
}

func PollForLogin(client *http.Client, serverHost string, token string) (*LoginResult, error) {
	url := fmt.Sprintf("https://%s/api/login/poll", serverHost)

	data := map[string]string{
		"token": token,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	maxRetries := 60
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
		req.Header.Set("Referer", fmt.Sprintf("https://%s/login", serverHost))
		req.Header.Set("X-Requested-With", "XMLHttpRequest")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
		}

		var pollResponse PollResponse
		if err := json.Unmarshal(body, &pollResponse); err != nil {
			return nil, err
		}

		if pollResponse.Success {
			if resultMap, ok := pollResponse.Result.(map[string]interface{}); ok {
				if _, ok := resultMap["uid"]; ok {
					resultJSON, err := json.Marshal(pollResponse.Result)
					if err != nil {
						return nil, err
					}

					var loginResult LoginResult
					if err := json.Unmarshal(resultJSON, &loginResult); err != nil {
						return nil, err
					}

					return &loginResult, nil
				}
			}
		}

		time.Sleep(1 * time.Second)
	}

	return nil, errors.New("timeout waiting for QR code scan")
}

func GetAppInfo(client *http.Client, serverHost string) (*AppInfoResponse, error) {
	url := fmt.Sprintf("https://%s/api/customized/web/app/info", serverHost)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/playback", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var appInfoResponse AppInfoResponse
	if err := json.Unmarshal(body, &appInfoResponse); err != nil {
		return nil, err
	}

	if !appInfoResponse.Success {
		return nil, errors.New(appInfoResponse.Msg)
	}

	return &appInfoResponse, nil
}

func GetMQTTConfig(client *http.Client, serverHost string) (*MQTTConfigResponse, error) {
	url := fmt.Sprintf("https://%s/api/jarvis/mqtt", serverHost)

	req, err := http.NewRequest("POST", url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/playback", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var mqttConfigResponse MQTTConfigResponse
	if err := json.Unmarshal(body, &mqttConfigResponse); err != nil {
		return nil, err
	}

	if !mqttConfigResponse.Success {
		return nil, errors.New(mqttConfigResponse.Msg)
	}

	return &mqttConfigResponse, nil
}

func GetWebRTCConfig(client *http.Client, serverHost string, deviceId string) (*WebRTCConfigResponse, error) {
	url := fmt.Sprintf("https://%s/api/jarvis/config", serverHost)

	data := map[string]string{
		"devId":         deviceId,
		"clientTraceId": fmt.Sprintf("%x", rand.Int63()),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/playback", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var webRTCConfigResponse WebRTCConfigResponse
	if err := json.Unmarshal(body, &webRTCConfigResponse); err != nil {
		return nil, err
	}

	if !webRTCConfigResponse.Success {
		return nil, errors.New(webRTCConfigResponse.Msg)
	}

	return &webRTCConfigResponse, nil
}

func GetHomeList(client *http.Client, serverHost string) (*HomeListResponse, error) {
	url := fmt.Sprintf("https://%s/api/new/common/homeList", serverHost)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/playback", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var homeListResponse HomeListResponse
	if err := json.Unmarshal(body, &homeListResponse); err != nil {
		return nil, err
	}

	if !homeListResponse.Success {
		return nil, errors.New(homeListResponse.Msg)
	}

	return &homeListResponse, nil
}

func GetSharedHomeList(client *http.Client, serverHost string) (*SharedHomeListResponse, error) {
	url := fmt.Sprintf("https://%s/api/new/playback/shareList", serverHost)

	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/playback", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var sharedHomeListResponse SharedHomeListResponse
	if err := json.Unmarshal(body, &sharedHomeListResponse); err != nil {
		return nil, err
	}

	if !sharedHomeListResponse.Success {
		return nil, errors.New(sharedHomeListResponse.Msg)
	}

	return &sharedHomeListResponse, nil
}

func GetRoomList(client *http.Client, serverHost string, homeId string) (*RoomListResponse, error) {
	url := fmt.Sprintf("https://%s/api/new/common/roomList", serverHost)

	data := map[string]string{
		"homeId": homeId,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/playback", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d - %s", resp.StatusCode, string(body))
	}

	var roomListResponse RoomListResponse
	if err := json.Unmarshal(body, &roomListResponse); err != nil {
		return nil, err
	}

	if !roomListResponse.Success {
		return nil, errors.New(roomListResponse.Msg)
	}

	return &roomListResponse, nil
}

func performLogin(client *http.Client, url string, loginReq PasswordLoginRequest, serverHost string) (*PasswordLoginResponse, error) {
	jsonData, err := json.Marshal(loginReq)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Origin", fmt.Sprintf("https://%s", serverHost))
	req.Header.Set("Referer", fmt.Sprintf("https://%s/login", serverHost))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var loginResp PasswordLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return nil, err
	}

	return &loginResp, nil
}

func encryptPassword(password, pbKey string) (string, error) {
	// Hash password with MD5
	hasher := md5.New()
	hasher.Write([]byte(password))
	hashedPassword := hex.EncodeToString(hasher.Sum(nil))

	// Decode PEM public key
	block, _ := pem.Decode([]byte("-----BEGIN PUBLIC KEY-----\n" + pbKey + "\n-----END PUBLIC KEY-----"))
	if block == nil {
		return "", errors.New("failed to decode PEM block")
	}

	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}

	rsaPubKey, ok := pubKey.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("not an RSA public key")
	}

	// Encrypt with RSA
	encrypted, err := rsa.EncryptPKCS1v15(cryptoRand.Reader, rsaPubKey, []byte(hashedPassword))
	if err != nil {
		return "", err
	}

	// Convert to hex string
	return hex.EncodeToString(encrypted), nil
}

func isEmailAddress(input string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(input)
}
