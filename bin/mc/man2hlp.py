#!/usr/bin/env python3
"""mc's man2hlp in Python: a manual page and a template to mc's help file.

    man2hlp.py <file.man> <template_file> <helpfile>

mc's own is a Perl script (src/man2hlp/man2hlp.in), and the build has no
Perl: this is that script, line for line, its state the same globals,
its strtok the same emulation of C's, its bytes the same bytes (Latin-1
keeps each a character, as Perl's strings of a file's have them). The
help file is the manual's text, its sections the nodes the help viewer
links, then the template's (xnc.hlp), the [Contents] node first.

Copyright (C) 1994-2025 The Free Software Foundation, Inc.
Originally written by Andrew V. Samoilov, 2002, Pavel Roskin, 2002 and
Andrew Borodin <aborodin@vmail.ru>, 2010; in Perl by Alexandr Prenko,
2010. This program is free software: you can redistribute it and/or
modify it under the terms of the GNU General Public License as published
by the Free Software Foundation, either version 3 of the License, or (at
your option) any later version.
"""
import re
import sys

CHAR_LINK_START = "\x01"
CHAR_LINK_POINTER = "\x02"
CHAR_LINK_END = "\x03"
CHAR_NODE_END = "\x04"
CHAR_FONT_BOLD = "\x08"
CHAR_FONT_NORMAL = "\x0b"
CHAR_FONT_ITALIC = "\x14"


class Man2hlp:
    def __init__(self):
        self.anchor_flag = False  # string_len's: inside a node's anchor name
        self.lc_link_flag = False  # string_len's: inside a link's target name
        self.old = None  # handle_link's first part of a link
        self.col = 0  # the output's column
        self.out_row = 1  # the output's row
        self.in_row = 0  # the input's row
        self.no_split_flag = False  # don't split the section on the next .SH
        self.skip_flag = 0  # 1: skipping a title, 2: the title skipped, skipping the text
        self.link_flag = 0  # the next line is a link
        self.verbatim_flag = False  # copy the input to the output verbatim
        self.node = False  # this line is an original .SH
        self.out = []
        self.size = 0  # the output's length, tell's
        self.c_in = None  # the input's name
        self.indentation = 0
        self.tp_flag = 0  # 1: this line is .TP's label, 2: the first line of its description
        self.topics = None
        self.strtok_rest = None
        self.nodes = []  # each {node, lname, heading_level}
        self.links = []  # each {linkname, filename, line}

    def write(self, text):
        self.out.append(text)
        self.size += len(text)

    def strtok(self, text, chars):
        """C's strtok: with no delimiters, the rest of the string."""
        if not chars:
            result, self.strtok_rest = self.strtok_rest, None

            return result

        if text is None:
            text = self.strtok_rest

        if text is None:
            return None

        pattern = "[" + re.escape(chars) + "]+"
        text = re.sub("^" + pattern, "", text)
        parts = re.split(pattern, text, maxsplit=1)
        result, self.strtok_rest = parts[0], parts[1] if len(parts) > 1 else None

        if result == "" and self.strtok_rest is not None:
            parts = re.split(pattern, self.strtok_rest, maxsplit=1)
            result, self.strtok_rest = parts[0], parts[1] if len(parts) > 1 else None

        if self.strtok_rest == "":
            self.strtok_rest = None

        return result

    def print_error(self, message):
        sys.stderr.write(f'man2hlp: {message} in file "{self.c_in}" on line {self.in_row}\n')

    def newline(self):
        self.out_row += 1
        self.col = 0
        self.write("\n")

    def string_len(self, buffer):
        backslash_flag = False
        length = 0

        for c in buffer:
            if c == CHAR_LINK_POINTER:
                self.lc_link_flag = True
            elif c == CHAR_LINK_END:
                self.lc_link_flag = False
            elif c == CHAR_NODE_END:
                self.anchor_flag = True
                # the script's hack against the loss of a space
                length += 1

            if ord(c) < 32:
                continue

            if c == "\\" and not backslash_flag:
                backslash_flag = True
                continue

            backslash_flag = False

            if not self.anchor_flag and not self.lc_link_flag:
                length += 1

            if self.anchor_flag and c == "]":
                self.anchor_flag = False

        return length

    def print_string(self, buffer):
        backslash_flag = False
        font_change_flag = False
        quotes_flag = 0

        if self.skip_flag:
            return

        if self.verbatim_flag:
            for c in buffer:
                if c == "\\" and not backslash_flag:
                    backslash_flag = True
                    continue

                backslash_flag = False
                self.write(c)

            return

        word = self.strtok(buffer, " \t\n")

        while word is not None:
            if word != "":
                length = self.string_len(word)

                if self.col > 0:
                    self.write(" ")
                    self.col += 1
                elif self.indentation:
                    while True:
                        go = self.col < self.indentation
                        self.col += 1

                        if not go:
                            break

                        self.write(" ")

                for c in word:
                    # \(lq, \(rq and \(dq
                    if quotes_flag != 0:
                        if c in "lrd" and quotes_flag == 1:
                            quotes_flag = 2
                            continue

                        if c == "q" and quotes_flag == 2:
                            quotes_flag = 0
                            self.write('"')
                            continue

                        self.write("(" + c)
                        self.print_error(f"Syntax error: unsupported \\({c} command")

                    # \fR, \fB, \fI and \fP
                    if font_change_flag:
                        if c == "B":
                            self.write(CHAR_FONT_BOLD)
                        elif c == "I":
                            self.write(CHAR_FONT_ITALIC)
                        elif c in "RP":
                            self.write(CHAR_FONT_NORMAL)
                        else:
                            self.write("f" + c)
                            self.print_error(f"Syntax error: unsupported \\f{c} command")

                        font_change_flag = False
                        continue

                    if c == "(" and backslash_flag:
                        quotes_flag = 1
                        backslash_flag = False
                        continue

                    if c == "f" and backslash_flag:
                        font_change_flag = True
                        backslash_flag = False
                        continue

                    if c == "\\" and not backslash_flag:
                        backslash_flag = True
                        continue

                    backslash_flag = False
                    font_change_flag = False
                    quotes_flag = 0
                    self.write(c)

                self.col += length

            word = self.strtok(None, " \t\n")

    def add_node(self, name, heading_level):
        self.nodes.append({"node": name, "lname": None, "heading_level": heading_level})

    def handle_node(self, is_sh):
        if self.skip_flag == 2:
            self.skip_flag = 0

        buffer = self.strtok(None, "")

        if buffer is None:
            self.print_error("Syntax error: .SH: no title")
            return

        if buffer.startswith('"'):
            buffer = buffer[1:]

            if buffer.endswith('"'):
                buffer = buffer[:-1]

        heading_level = len(buffer) - len(buffer.lstrip(" "))

        if heading_level % 2:
            self.print_error("Syntax error: .SH: odd heading level")

        if self.no_split_flag:
            self.newline()
            self.print_string(buffer)
            self.newline()
            self.newline()
            self.no_split_flag = False
        elif self.skip_flag:
            self.skip_flag = 2
        else:
            buffer = buffer[heading_level:]

            if not is_sh or not self.node:
                if buffer != "":
                    self.write(f"{CHAR_NODE_END}[{buffer}]")
                    self.newline()

                self.add_node(buffer, heading_level)

            if is_sh:
                self.nodes[-1]["lname"] = buffer
                self.print_string(buffer)
                self.newline()
                self.newline()

        self.node = not is_sh

    @staticmethod
    def char_to_font(c):
        return {"R": CHAR_FONT_NORMAL, "B": CHAR_FONT_BOLD, "I": CHAR_FONT_ITALIC}.get(c, "\0")

    def handle_alt_font(self, buffer):
        """.BR, .IR, .RB, .RI, .BI and .IB: whether it was one of them."""
        in_quotes = False
        alt_state = 0

        if len(buffer) != 3 or buffer[0] != ".":
            return False

        font = [self.char_to_font(buffer[1]), self.char_to_font(buffer[2])]

        if font[0] == "\0" or font[1] == "\0" or font[0] == font[1]:
            return False

        p = self.strtok(None, "")

        if p is None:
            return True

        buffer = font[0]
        p = list(p)

        while p:
            if p[0] == '"':
                in_quotes = not in_quotes
                p.pop(0)
                continue

            if p[0] == " " and not in_quotes:
                p.pop(0)

                if p:
                    alt_state = 0 if alt_state else 1
                    buffer += font[alt_state]

                while p and p[0] == " ":
                    p.pop(0)

                continue

            buffer += p.pop(0)

        if font[alt_state] != CHAR_FONT_NORMAL:
            buffer += CHAR_FONT_NORMAL

        self.print_string(buffer)

        return True

    def handle_tp_ip(self, is_tp):
        if self.col > 0:
            self.newline()

        self.newline()

        if is_tp:
            self.tp_flag = 1
            self.indentation = 0
        else:
            self.indentation = 8

    def handle_command(self, buffer):
        buffer = self.strtok(buffer, " \t")

        if buffer == ".SH":
            self.indentation = 0
            self.handle_node(True)
        elif buffer == '.\\"NODE':
            self.handle_node(False)
        elif buffer == '.\\"DONT_SPLIT"':
            self.no_split_flag = True
        elif buffer == '.\\"SKIP_SECTION"':
            self.skip_flag = 1
        elif buffer == '.\\"LINK2"':
            self.link_flag = 2
        elif buffer in (".PP", ".P", ".LP"):
            self.indentation = 0

            if self.col > 0:
                self.newline()

            self.newline()
        elif buffer == ".nf":
            self.verbatim_flag = True

            if self.col > 0:
                self.newline()
        elif buffer in (".I", ".B", ".SB"):
            backslash_flag = False
            p = self.strtok(None, "")

            if p is None:
                self.print_error("Syntax error: .I | .B | .SB : no text")
                return

            buffer = CHAR_FONT_ITALIC if buffer[1] == "I" else CHAR_FONT_BOLD

            for c in p:
                if c == "\\" and not backslash_flag:
                    backslash_flag = True
                    continue

                backslash_flag = False
                buffer += c

            self.print_string(buffer + CHAR_FONT_NORMAL)
        elif buffer == ".TP":
            self.handle_tp_ip(True)
        elif buffer == ".IP":
            self.handle_tp_ip(False)
        elif buffer == '.\\"TOPICS':
            if self.out_row > 1:
                self.print_error('Syntax error: .\\"TOPICS must be first command')
                return

            buffer = self.strtok(None, "")

            if buffer is None:
                self.print_error('Syntax error: .\\"TOPICS: no text')
                return

            if buffer.startswith('"'):
                buffer = buffer[1:]

                if buffer.endswith('"'):
                    buffer = buffer[:-1]

            self.topics = buffer
        elif buffer == ".br":
            if self.col:
                self.newline()
        elif buffer.startswith('.\\"') or buffer == ".TH":
            pass
        elif buffer == ".SM":
            buffer = self.strtok(None, "")

            if buffer is not None:
                self.print_string(buffer)
        elif self.handle_alt_font(buffer):
            return
        elif buffer == ".RE":
            self.newline()
        else:
            self.print_error(f"Warning: unsupported command {buffer}")

    def handle_link(self, buffer):
        if self.link_flag == 2:
            if buffer[:2] in (".I", ".B"):
                buffer = re.sub(r"^..[\s\t]*", "", buffer)

            self.old = buffer
            self.link_flag = 3
        elif self.link_flag == 3:
            buffer = re.sub(r"^\.", "", buffer)
            buffer = re.sub(r"^\\", "", buffer)
            buffer = re.sub(r'^"', "", buffer)
            buffer = re.sub(r'"$', "", buffer)
            # "Layout\&)," -- "Layout" should be highlighted, but not "),"
            old, _, amp_arg = self.old.partition("\\&")
            self.old = old
            self.print_string(f"{CHAR_LINK_START}{old}{CHAR_LINK_POINTER}{buffer}{CHAR_LINK_END}{amp_arg}\n")
            self.link_flag = 0
            self.links.append({"linkname": buffer, "filename": self.c_in, "line": self.in_row})

    def manual(self, name, text):
        """The first stage: the manual to the output."""
        self.c_in = name

        for buffer in text.split("\n")[:-1] if text.endswith("\n") else text.split("\n"):
            input_line = buffer[2:] if buffer.startswith("\\&") else buffer
            self.in_row += 1

            if self.verbatim_flag:
                if input_line == ".fi":
                    self.verbatim_flag = False
                else:
                    self.print_string(input_line)
                    self.newline()
            elif self.link_flag:
                self.handle_link(input_line)
            elif buffer.startswith("."):
                self.handle_command(input_line)
            else:
                self.print_string(input_line)

            # .TP's label is a line as any other
            if self.tp_flag:
                if self.tp_flag == 1:
                    self.tp_flag = 2
                else:
                    self.tp_flag = 0
                    self.indentation = 8

                    if self.col >= self.indentation:
                        self.newline()
                    else:
                        while True:
                            self.col += 1

                            if not self.col < self.indentation:
                                break

                            self.write(" ")

        self.newline()

    def template(self, name, text):
        """The second stage: the template after the manual, its nodes."""
        self.c_in = name
        lc_node = None

        for buffer in text.splitlines(keepends=True):
            if lc_node is not None:
                if buffer != "\n":
                    self.nodes[-1]["lname"] = buffer.rstrip("\n")

                lc_node = None
            else:
                at = buffer.find(CHAR_NODE_END)
                lc_node = None if at < 0 else buffer[at:]

                if lc_node is not None and lc_node[1:2] == "[":
                    p = lc_node.find("]")

                    if p >= 0 and lc_node[1:7] != "[main]":
                        self.add_node(lc_node[2:p], 0)
                    else:
                        lc_node = None
                else:
                    lc_node = None

            self.write(buffer)

    def contents(self):
        """The [Contents] node, of every node; a link to no node is an error."""
        cont_start = self.size
        self.write(f"\x04[Contents]\n{self.topics}\n\n" if self.topics else "\x04[Contents]\n")
        names = {node["node"] for node in self.nodes}

        for link in self.links:
            if link["linkname"] != "Contents" and link["linkname"] not in names:
                self.c_in, self.in_row = link["filename"], link["line"]
                self.print_error(f'Stale link "{link["linkname"]}"')

        for node in self.nodes:
            if node["node"]:
                self.write("  " + " " * node["heading_level"] + f"\x01{node['lname'] or node['node']}\x02{node['node']}\x03")

            self.write("\n")

        return cont_start


def convert(manual, template):
    """The help file of a manual's text and a template's, both Latin-1."""
    m = Man2hlp()
    m.manual("manual", manual)
    m.template("template", template)
    cont_start = m.contents()
    text = "".join(m.out)

    # the third stage: [Contents] and after, then what was before it but its
    # last character
    return text[cont_start:] + text[:cont_start - 1] + "\n"


def main():
    if len(sys.argv) != 4:
        raise SystemExit("Usage: man2hlp file.man template_file helpfile")

    manual, template, output = sys.argv[1:]

    with open(manual, encoding="latin-1", newline="") as f, open(template, encoding="latin-1", newline="") as g:
        text = convert(f.read(), g.read())

    with open(output, "w", encoding="latin-1", newline="") as f:
        f.write(text)


if __name__ == "__main__":
    main()
