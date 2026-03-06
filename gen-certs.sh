#!/bin/bash

# Exit on error
set -e

# Disable Git Bash path conversion on Windows (prevents /C=US from becoming C:/Users/...)
export MSYS_NO_PATHCONV=1

# Create certs directory
mkdir -p certs

# Generate CA private key and certificate
openssl req -x509 -newkey rsa:4096 -days 365 -nodes \
  -keyout certs/ca-key.pem -out certs/ca-cert.pem \
  -subj "/C=US/ST=State/L=City/O=Organization/OU=CA/CN=ca.example.com"

# Create config file for server certificate with SAN
cat > certs/server-ext.cnf << 'EOF'
[req]
default_bits = 4096
prompt = no
default_md = sha384
req_extensions = req_ext
distinguished_name = dn

[dn]
C = US
ST = State
L = City
O = Organization
OU = Server
CN = localhost

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
DNS.2 = *.localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

# Generate server private key and certificate signing request
openssl req -newkey rsa:4096 -nodes \
  -keyout certs/server-key.pem -out certs/server-req.pem \
  -config certs/server-ext.cnf

# Sign server certificate with CA (with SAN extension)
openssl x509 -req -in certs/server-req.pem -days 365 \
  -CA certs/ca-cert.pem -CAkey certs/ca-key.pem -CAcreateserial \
  -out certs/server-cert.pem \
  -extfile certs/server-ext.cnf -extensions req_ext

# Create config file for USER client certificate with SAN
cat > certs/user-ext.cnf << 'EOF'
[req]
default_bits = 4096
prompt = no
default_md = sha384
req_extensions = req_ext
distinguished_name = dn

[dn]
C = US
ST = State
L = City
O = Organization
OU = Client
CN = user.example.com

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = user.example.com
EOF

# Generate USER client private key and certificate signing request
openssl req -newkey rsa:4096 -nodes \
  -keyout certs/user-key.pem -out certs/user-req.pem \
  -config certs/user-ext.cnf

# Sign USER client certificate with CA (with SAN extension)
openssl x509 -req -in certs/user-req.pem -days 365 \
  -CA certs/ca-cert.pem -CAkey certs/ca-key.pem -CAcreateserial \
  -out certs/user-cert.pem \
  -extfile certs/user-ext.cnf -extensions req_ext

# Create config file for ADMIN client certificate with SAN
cat > certs/admin-ext.cnf << 'EOF'
[req]
default_bits = 4096
prompt = no
default_md = sha384
req_extensions = req_ext
distinguished_name = dn

[dn]
C = US
ST = State
L = City
O = Organization
OU = Client
CN = admin.example.com

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = admin.example.com
EOF

# Generate Admin client private key and certificate signing request
openssl req -newkey rsa:4096 -nodes \
  -keyout certs/admin-key.pem -out certs/admin-req.pem \
  -config certs/admin-ext.cnf

# Sign Admin client certificate with CA (with SAN extension)
openssl x509 -req -in certs/admin-req.pem -days 365 \
  -CA certs/ca-cert.pem -CAkey certs/ca-key.pem -CAcreateserial \
  -out certs/admin-cert.pem \
  -extfile certs/admin-ext.cnf -extensions req_ext

# Create config file for UNKNOWN client certificate with SAN
cat > certs/unknown-ext.cnf << 'EOF'
[req]
default_bits = 4096
prompt = no
default_md = sha384
req_extensions = req_ext
distinguished_name = dn

[dn]
C = US
ST = State
L = City
O = Organization
OU = Client
CN = unknown.example.com

[req_ext]
subjectAltName = @alt_names

[alt_names]
DNS.1 = unknown.example.com
EOF

# Generate Unknown client private key and certificate signing request
openssl req -newkey rsa:4096 -nodes \
  -keyout certs/unknown-key.pem -out certs/unknown-req.pem \
  -config certs/unknown-ext.cnf

# Sign Unknown client certificate with CA (with SAN extension)
openssl x509 -req -in certs/unknown-req.pem -days 365 \
  -CA certs/ca-cert.pem -CAkey certs/ca-key.pem -CAcreateserial \
  -out certs/unknown-cert.pem \
  -extfile certs/unknown-ext.cnf -extensions req_ext

echo -e "\nCertificates generated successfully with SANs!"

# Verify the certificates have SANs
echo -e "\nServer certificate SANs:"
openssl x509 -in certs/server-cert.pem -text -noout | grep -A2 "Subject Alternative Name"

echo -e "\nUser certificate SANs:"
openssl x509 -in certs/user-cert.pem -text -noout | grep -A2 "Subject Alternative Name"

echo -e "\nAdmin certificate SANs:"
openssl x509 -in certs/admin-cert.pem -text -noout | grep -A2 "Subject Alternative Name"

echo -e "\nUnknown certificate SANs:"
openssl x509 -in certs/unknown-cert.pem -text -noout | grep -A2 "Subject Alternative Name"

# Cleanup intermediate files
rm -f certs/*-req.pem certs/*-ext.cnf certs/ca-cert.srl