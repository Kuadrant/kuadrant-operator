provider "aws" {
  region = "eu-west-1"
  access_key = var.aws_access_key
  secret_key = var.aws_secret_key
}

variable "aws_access_key" {
  description = "AWS Access Key"
  type        = string
}

variable "aws_secret_key" {
  description = "AWS Secret Key"
  type        = string
}

variable "aws_key_name" {
  description = "AWS Key Name"
  type = string
}

resource "aws_instance" "self_hosted_runner" {
  ami           = "ami-055032149717ffb30" # change to ami-0776c814353b4814d when creating an AMI. 
  instance_type = "t2.xlarge"

  root_block_device {
    volume_size = 16  // GB
  }

  key_name = var.aws_key_name
  
  tags = {
    Name = "kuadrant-operator-self-hosted-runner"
  }

  // Security Group for SSH, HTTP, and HTTPS access
  security_groups = ["ssh-http-https-access"]

  # Uncomment when creating an AMI . 

  /* user_data = <<-EOL
    #!/bin/bash
    echo "Starting user_data script..."
    sudo apt-get update -y
    sudo apt-get install -y podman golang libicu-dev expect
    sudo snap install yq
    curl -O https://s3.us-west-2.amazonaws.com/amazon-eks/1.30.0/2024-05-12/bin/linux/amd64/kubectl
    chmod +x ./kubectl
    mkdir -p /home/ubuntu/bin && cp ./kubectl /home/ubuntu/bin/kubectl
    echo 'alias podman="sudo podman"' >> /home/ubuntu/.bashrc
    echo export PATH=/home/ubuntu/bin:/home/ubuntu/go/pkg/mod/bin:$PATH >> /home/ubuntu/.bashrc
    source /home/ubuntu/.bashrc
    export GOMODCACHE=/home/ubuntu/go
    export GOPATH=/home/ubuntu/go/pkg/mod
    export GOCACHE=/home/ubuntu/.cache/go-build
    export HOME=/home/ubuntu
    go install sigs.k8s.io/kind@v0.23.0
    source /home/ubuntu/.bashrc
    cd /home/ubuntu
    git clone https://www.github.com/kuadrant/kuadrant-operator.git
    echo 'unqualified-search-registries = ["docker.io"]' | sudo tee -a /etc/containers/registries.conf
    sudo chmod 7777 kuadrant-operator/hack
    echo "user_data script execution completed."
    touch /tmp/user_data_done
  EOL */
}



resource "aws_security_group" "ssh_http_https_access" {
  name        = "ssh-http-https-access"
  description = "Allow SSH, HTTP, and HTTPS access"
  
  // Ingress rule for SSH access
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]  // Allow SSH access from anywhere
  }

  // Ingress rule for HTTP access (port 80)
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]  // Allow HTTP access from anywhere
  }

  // Ingress rule for HTTPS access (port 443)
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]  // Allow HTTPS access from anywhere
  }

  // Egress rule to allow all outbound traffic
  egress {
    from_port       = 0
    to_port         = 0
    protocol        = "-1"  // Allow all protocols
    cidr_blocks     = ["0.0.0.0/0"]  // Allow outbound traffic to anywhere
  }
}

resource "null_resource" "wait_for_user_data" {
  provisioner "local-exec" {
    command = <<EOT
    while ! ssh -o StrictHostKeyChecking=no -i ${aws_instance.self_hosted_runner.key_name}.pem ubuntu@${aws_instance.self_hosted_runner.public_ip} 'test -f /tmp/user_data_done'; do
      echo "Waiting for user_data script to complete..."
      sleep 10
    done
    echo "user_data script completed."
    EOT
  }

  depends_on = [aws_instance.self_hosted_runner]
}

output "instance_public_ip" {
  value = aws_instance.self_hosted_runner.public_ip
}
