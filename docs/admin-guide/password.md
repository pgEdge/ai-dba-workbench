# Enforcing the Password Policy

The server validates every new or updated password against a policy aligned
with the NIST SP 800-63B [Digital Identity Guidelines][nist-800-63b]. The
server is the authoritative validator; the web client mirrors these checks
to provide live feedback as the user types.

[nist-800-63b]: https://pages.nist.gov/800-63-3/sp800-63b.html

The server enforces both a minimum and a maximum password length.

- A password must contain at least 12 characters.
- A password must not exceed 72 bytes, which is the hard limit imposed by
  the bcrypt hashing algorithm.
- The 72-byte cap matches the byte length rather than the character count;
  multi-byte UTF-8 characters consume more than one byte each and reduce
  the effective character count accordingly.

The policy intentionally omits character-class requirements such as
mandatory uppercase, lowercase, digit, or special-character mixes. NIST SP
800-63B recommends against composition rules because they push users toward
predictable substitutions like `Password123` or `Summer2026!` and reject
high-entropy passphrases that are easier to remember and harder to guess.

The server rejects any password that appears in a built-in dictionary of
approximately 10,000 common and breached passwords. Choose a passphrase
composed of several unrelated words to maximize entropy while avoiding
entries the dictionary already covers.

The server returns a validation error and refuses to store any password
that fails one of the checks below.

- A password shorter than 12 characters is rejected as too short.
- A password longer than 72 bytes is rejected as too long.
- A password matching an entry in the common-password dictionary is
  rejected as too common.

The web client evaluates each keystroke in the password field and displays
a strength indicator alongside any policy violations. The indicator is a
usability aid; the server independently re-validates the password before
storing the bcrypt hash.


## Examples

The following passphrase satisfies every rule because the unrelated words
and trailing characters yield high entropy without appearing in the
dictionary:

```text
correct-horse-battery-staple-9z
```

The next two passwords look strong under traditional composition rules, but
they follow common patterns that frequently appear in password dictionaries;
modern policies treat such passwords as weak:

```text
Password123!
Summer2026!
```

The validator rejects these passwords because they match the embedded
common-password list; otherwise it relies on the 12-character minimum to
discourage shorter variants. Avoid these patterns even when a specific
example slips past the dictionary check.

