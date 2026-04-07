package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	// Store video files in S3. Images will stay on local file system for now.

	// validate user and get ID
	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	// set upload limit of 1GB
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

	// extract videoID from URL path parameters and parse as UUID
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	// get video metadata from database
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to do that", nil)
		return
	}

	// parse video file from the form data
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not parse video file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}

	// save uploaded file to a temporary file on disk
	tempFile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not save to temp file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// copy the contents from the wire to the temp file
	if _, err := io.Copy(tempFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not copy to temp file", err)
		return
	}

	// get aspect ratio for key prefix
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't get video aspect ratio", err)
		return
	}

	// reset tempFile's file pointer to the beginning
	// allows us to read the file again from the beginning
	if _, err = tempFile.Seek(0, io.SeekStart); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Could not reset temp file pointer to beginning", err)
		return
	}

	// put object into S3
	base := make([]byte, 16)
	if _, err = rand.Read(base); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate random bytes", err)
		return
	}
	baseKey := hex.EncodeToString(base) + mediaTypeToExt(mediaType)
	prefix := "other/"
	if aspectRatio == "9:16" {
		prefix = "portrait/"
	}
	if aspectRatio == "16:9" {
		prefix = "landscape/"
	}
	key := prefix + baseKey
	params := s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(key),
		Body:        tempFile,
		ContentType: aws.String(mediaType),
	}
	_, err = cfg.s3Client.PutObject(r.Context(), &params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload video to S3", err)
		return
	}

	// update videoURL in database with the s3 bucket and key
	url := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, key)
	video.VideoURL = aws.String(url)
	if err = cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
