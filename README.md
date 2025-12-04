# ABR-TEST

**All the videos are transcoded to hls only for keeping things simple**

**video.js and shaka-player support both hls and dash**

## Player libraries used
- [video.js](https://videojs.org/)
- [shaka-player](https://github.com/video-dev/hls.js)
- [hls.js](https://github.com/video-dev/hls.js)

# Setup
- create videos directory in the root and put all your videos there
- transcoded versions will be placed in transcoded directory
- open index.html in a browser and you can view,transcode and check status on that page


```bash
# Start server
go run main.go

# Add a video to videos/ folder, then:
curl -X POST "http://localhost:8000/api/transcode?video=myvideo.mp4"

# Check status
curl "http://localhost:8000/api/status/myvideo.mp4"

# Get all videos
curl "http://localhost:8000/api/videos"

# Play in browser
http://localhost:8000/hls/myvideo/master.m3u8
```

