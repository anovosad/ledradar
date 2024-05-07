# Use an official Golang runtime as a parent image
FROM golang:1.22.2-alpine

# Set the working directory in the container to /app
WORKDIR /app

# Copy the current directory contents into the container at /app
COPY . /app

# Build the Go app
RUN go build -o ledradar .

# Make port 80 available to the world outside this container
EXPOSE 8080

# Run ledradar when the container launches
ENTRYPOINT ["/app/ledradar"]