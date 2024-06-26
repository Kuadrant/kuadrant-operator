data "aws_instance" "self_hosted_runner_instance" {
  instance_id = aws_instance.self_hosted_runner.id
  depends_on = [ null_resource.wait_for_user_data ]
}

resource "aws_ami_from_instance" "self_hosted_runner_ami" {
  name               = "self-hosted-runner-ami"
  source_instance_id = data.aws_instance.self_hosted_runner_instance.id
  description        = "An AMI created from an existing EC2 instance which contains the environment needed for self-hosted runner on kuadrant-operator."

  tags = {
    Name = "self-hosted-runner-ami"
  }

  lifecycle {
    prevent_destroy = true
  }

  depends_on = [ null_resource.wait_for_user_data ]
}