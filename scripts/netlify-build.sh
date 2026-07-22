#!/bin/sh
set -eu

rm -rf dist
mkdir -p dist
cp index.html login.html app.html superadmin.html site.css styles.css site.js script.js app-api.js superadmin.js dist/
cp -R assets dist/assets
