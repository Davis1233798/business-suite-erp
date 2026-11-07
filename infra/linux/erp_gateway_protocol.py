"""Narrow command protocol between the ERP SSH and runtime accounts."""

from erp_image_release import release_reference

MAX_REQUEST = 128


def parse_request(request):
    if len(request) > MAX_REQUEST:
        raise ValueError("ERP gateway request is too long")
    if request == "status":
        return ("status",)
    parts = request.split(" ")
    if len(parts) != 3 or parts[0] != "deploy":
        raise ValueError("ERP gateway accepts only status or deploy with a commit and digest")
    release_reference(parts[1], parts[2])
    return tuple(parts)
