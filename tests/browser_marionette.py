"""Bounded Marionette transport shared by local browser qualification fixtures."""
import json
import socket


class MarionetteClient:
    def __init__(self, address="127.0.0.1:2828"):
        host, port = address.rsplit(":", 1)
        self.socket = socket.create_connection((host, int(port)), 10)
        self.socket.settimeout(30)
        self.receive()  # protocol greeting

    def receive(self):
        size = bytearray()
        while not size.endswith(b":"):
            value = self.socket.recv(1)
            if not value or len(size) > 10:
                raise RuntimeError("invalid Marionette frame")
            size.extend(value)
        length = int(size[:-1])
        if length > 4 * 1024 * 1024:
            raise RuntimeError("Marionette response exceeds fixture limit")
        data = bytearray()
        while len(data) < length:
            value = self.socket.recv(length - len(data))
            if not value:
                raise RuntimeError("Marionette connection closed")
            data.extend(value)
        return json.loads(data)

    def call(self, identifier, name, params):
        raw = json.dumps([0, identifier, name, params], separators=(",", ":")).encode()
        self.socket.sendall(str(len(raw)).encode() + b":" + raw)
        return self.receive()

    def close(self):
        self.socket.close()
