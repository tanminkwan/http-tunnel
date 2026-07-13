from flask import Flask, request

app = Flask(__name__)

@app.route('/')
def index():
    return {
        "message": "Hello from Internal Flask Server (via HTTP Tunnel)!",
        "location": "Client Side (Internal / NAT)",
        "headers": dict(request.headers),
        "status": "tunneled_access"
    }

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=5000)
