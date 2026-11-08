package s2

func task08() Task {
	return Task{
		ID:     "s2-08-member-signup",
		Set:    "tuning",
		System: system("a member directory"),
		Files: map[string]string{
			"go.mod": goMod,
			"directory.go": `package accounts

// User is one member.
type User struct {
	Name  string
	Email string
}

// Directory holds members by name.
type Directory struct {
	users map[string]User
}

// NewDirectory returns an empty Directory.
func NewDirectory() *Directory { return &Directory{users: map[string]User{}} }

// Get returns a member by name.
func (d *Directory) Get(name string) (User, bool) {
	u, ok := d.users[name]
	return u, ok
}
`,
			"policy/reserved.txt": `Reserved usernames

These names are kept for staff and must never be given to a member, whatever
letter case the member types: admin, root, support, billing.

Reviewed every quarter.
`,
			"directory_test.go": `package accounts

import "testing"

func TestGetMissing(t *testing.T) {
	if _, ok := NewDirectory().Get("nobody"); ok {
		t.Fatal("found a member in an empty directory")
	}
}
`,
		},
		Hidden: map[string]string{hiddenName: `package accounts_test

import (
	"errors"
	"testing"

	accounts "fixture"
)

func withAnna(t *testing.T) *accounts.Directory {
	t.Helper()
	d := accounts.NewDirectory()
	if err := d.Register("anna", "anna@example.org"); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSizeAndNames(t *testing.T) {
	d := withAnna(t)
	if err := d.Register("bert", "bert@example.org"); err != nil {
		t.Fatal(err)
	}
	if d.Size() != 2 {
		t.Fatalf("Size = %d", d.Size())
	}
	if got := d.Names(); len(got) != 2 || got[0] != "anna" || got[1] != "bert" {
		t.Fatalf("Names = %v", got)
	}
}

func TestLengthBounds(t *testing.T) {
	for name, want := range map[string]bool{
		"ab": false, "abc": true, "abcdefghijklmnop": true, "abcdefghijklmnopq": false,
	} {
		if got := accounts.LengthOK(name); got != want {
			t.Errorf("LengthOK(%q) = %v", name, got)
		}
	}
}

func TestLengthCountsCharacters(t *testing.T) {
	for name, want := range map[string]bool{
		"日本": false, "日本語のなまえです": true, "日本語のなまえです日本語のなまえです": false,
	} {
		if got := accounts.LengthOK(name); got != want {
			t.Errorf("LengthOK(%q) = %v", name, got)
		}
	}
}

func TestRegisterAdds(t *testing.T) {
	if u, ok := withAnna(t).Get("anna"); !ok || u.Email != "anna@example.org" {
		t.Fatalf("Get = %+v, %v", u, ok)
	}
}

func TestRegisterTaken(t *testing.T) {
	if err := withAnna(t).Register("anna", "other@example.org"); err == nil {
		t.Fatal("no error")
	}
}

func TestRegisterEmptyNameRefused(t *testing.T) {
	d := accounts.NewDirectory()
	if err := d.Register("", "x@example.org"); err == nil {
		t.Fatal("no error")
	}
	if d.Size() != 0 {
		t.Fatalf("Size = %d", d.Size())
	}
}

func TestRegisterInvalidSentinel(t *testing.T) {
	err := accounts.NewDirectory().Register("", "x@example.org")
	if !errors.Is(err, accounts.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestRenameInvalidSentinel(t *testing.T) {
	if err := withAnna(t).Rename("anna", ""); !errors.Is(err, accounts.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegisterRefusesReserved(t *testing.T) {
	d := accounts.NewDirectory()
	for _, name := range []string{"admin", "Admin", "SUPPORT", "billing"} {
		if err := d.Register(name, "x@example.org"); err == nil {
			t.Errorf("%q was accepted", name)
		}
	}
}

func TestRenameRefusesReserved(t *testing.T) {
	d := withAnna(t)
	if err := d.Rename("anna", "ROOT"); err == nil {
		t.Fatal("no error")
	}
	if _, ok := d.Get("anna"); !ok {
		t.Fatal("a refused rename changed the member")
	}
}

func TestRegisterNormalisesEmail(t *testing.T) {
	d := accounts.NewDirectory()
	if err := d.Register("anna", "  Anna@Example.ORG "); err != nil {
		t.Fatal(err)
	}
	if u, _ := d.Get("anna"); u.Email != "anna@example.org" {
		t.Fatalf("stored %q", u.Email)
	}
}

func TestByEmailExact(t *testing.T) {
	if u, ok := withAnna(t).ByEmail("anna@example.org"); !ok || u.Name != "anna" {
		t.Fatalf("ByEmail = %+v, %v", u, ok)
	}
	if _, ok := withAnna(t).ByEmail("nobody@example.org"); ok {
		t.Fatal("found a missing address")
	}
}

func TestByEmailNormalises(t *testing.T) {
	if u, ok := withAnna(t).ByEmail(" ANNA@example.org "); !ok || u.Name != "anna" {
		t.Fatalf("ByEmail = %+v, %v", u, ok)
	}
}

func TestChangeEmailExact(t *testing.T) {
	d := withAnna(t)
	if err := d.ChangeEmail("anna", "new@example.org"); err != nil {
		t.Fatal(err)
	}
	if u, _ := d.Get("anna"); u.Email != "new@example.org" {
		t.Fatalf("stored %q", u.Email)
	}
}

func TestChangeEmailUnknown(t *testing.T) {
	if err := withAnna(t).ChangeEmail("ghost", "new@example.org"); err == nil {
		t.Fatal("no error")
	}
}

func TestChangeEmailNormalises(t *testing.T) {
	d := withAnna(t)
	if err := d.ChangeEmail("anna", " NEW@Example.org"); err != nil {
		t.Fatal(err)
	}
	if u, _ := d.Get("anna"); u.Email != "new@example.org" {
		t.Fatalf("stored %q", u.Email)
	}
}

func TestRenameMoves(t *testing.T) {
	d := withAnna(t)
	if err := d.Rename("anna", "hanna"); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Get("anna"); ok {
		t.Error("the old name is still there")
	}
	if u, ok := d.Get("hanna"); !ok || u.Name != "hanna" || u.Email != "anna@example.org" {
		t.Errorf("Get = %+v, %v", u, ok)
	}
}

func TestRenameUnknownOrTaken(t *testing.T) {
	d := withAnna(t)
	if err := d.Register("bert", "bert@example.org"); err != nil {
		t.Fatal(err)
	}
	if err := d.Rename("ghost", "newname"); err == nil {
		t.Error("renaming an unknown name gave no error")
	}
	if err := d.Rename("anna", "bert"); err == nil {
		t.Error("renaming to a taken name gave no error")
	}
}
`},
		Solution: map[string]string{
			"directory.go": `package accounts

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// ErrInvalid is what callers check for when a username is refused.
var ErrInvalid = errors.New("invalid username")

// User is one member.
type User struct {
	Name  string
	Email string
}

// Directory holds members by name.
type Directory struct {
	users map[string]User
}

// NewDirectory returns an empty Directory.
func NewDirectory() *Directory { return &Directory{users: map[string]User{}} }

// Get returns a member by name.
func (d *Directory) Get(name string) (User, bool) {
	u, ok := d.users[name]
	return u, ok
}

// Size returns the number of members.
func (d *Directory) Size() int { return len(d.users) }

// Names returns the member names, sorted.
func (d *Directory) Names() []string {
	out := make([]string, 0, len(d.users))
	for name := range d.users {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// LengthOK reports whether a username has 3 to 16 characters.
func LengthOK(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 3 && n <= 16
}

func reserved(name string) bool {
	switch strings.ToLower(name) {
	case "admin", "root", "support", "billing":
		return true
	}
	return false
}

func checkName(name string) error {
	if !LengthOK(name) || reserved(name) {
		return ErrInvalid
	}
	return nil
}

func normEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// Register adds a member.
func (d *Directory) Register(name, email string) error {
	if err := checkName(name); err != nil {
		return err
	}
	if _, taken := d.users[name]; taken {
		return errors.New("name already taken")
	}
	d.users[name] = User{Name: name, Email: normEmail(email)}
	return nil
}

// ByEmail finds a member by email address.
func (d *Directory) ByEmail(email string) (User, bool) {
	email = normEmail(email)
	for _, u := range d.users {
		if u.Email == email {
			return u, true
		}
	}
	return User{}, false
}

// ChangeEmail replaces a member's email address.
func (d *Directory) ChangeEmail(name, email string) error {
	u, ok := d.users[name]
	if !ok {
		return errors.New("unknown name")
	}
	u.Email = normEmail(email)
	d.users[name] = u
	return nil
}

// Rename gives a member a new name.
func (d *Directory) Rename(name, newName string) error {
	u, ok := d.users[name]
	if !ok {
		return errors.New("unknown name")
	}
	if err := checkName(newName); err != nil {
		return err
	}
	if _, taken := d.users[newName]; taken {
		return errors.New("name already taken")
	}
	delete(d.users, name)
	u.Name = newName
	d.users[newName] = u
	return nil
}
`,
		},
		Turns: script(
			cloud("Policy for usernames, ahead of any validation code: a name is 3 to 16 characters long, and we count characters, not bytes, because names can be in any script. To start, add `func (d *Directory) Size() int`."),
			local("Read policy/reserved.txt and tell me what it asks of us. No code changes."),
			local("Add `var ErrInvalid = errors.New(\"invalid username\")`. It's what callers check for whenever a username is refused, wherever that happens. Another convention: email addresses are stored trimmed and in lower case, and compared that way, because the mail system treats them so. No code for that yet. Also add `func (d *Directory) Names() []string`, sorted."),
			local("Add `func LengthOK(name string) bool`: whether a username's length is acceptable."),
			cloud("Add `func (d *Directory) Register(name, email string) error`, which adds a member. A name that is already taken is an error, and so is a name we don't accept."),
			local("Add `func (d *Directory) ByEmail(email string) (User, bool)`, which finds a member by email address."),
			cloud("Add `func (d *Directory) ChangeEmail(name, email string) error`. An unknown name is an error."),
			local("Add `func (d *Directory) Rename(name, newName string) error`. An unknown name, a taken name and a name we don't accept are all errors."),
		),
		Carries: []Carry{
			{
				What:        "usernames have 3 to 16 characters, counted in characters and not bytes",
				Kind:        CarryUser,
				Established: 0,
				Needed:      []int{3},
				Tests:       []string{"TestLengthBounds", "TestLengthCountsCharacters"},
				Absent:      []string{"16", "bytes", "characters", "rune"},
			},
			{
				What:        "reserved names are refused in any letter case (policy/reserved.txt)",
				Kind:        CarryDiscovered,
				Established: 1,
				Needed:      []int{4, 7},
				Source:      "policy/reserved.txt",
				Tests:       []string{"TestRegisterRefusesReserved", "TestRenameRefusesReserved"},
				Absent:      []string{"reserved", "admin", "staff"},
			},
			{
				What:        "ErrInvalid wherever a username is refused",
				Kind:        CarryEmbodied,
				Established: 2,
				Needed:      []int{4, 7},
				Tests:       []string{"TestRegisterInvalidSentinel", "TestRenameInvalidSentinel"},
				Absent:      []string{"errinvalid", "sentinel"},
			},
			{
				What:        "email addresses are stored and compared trimmed and in lower case",
				Kind:        CarryUser,
				Established: 2,
				Needed:      []int{4, 5, 6},
				Tests:       []string{"TestRegisterNormalisesEmail", "TestByEmailNormalises", "TestChangeEmailNormalises"},
				Absent:      []string{"trimmed", "lower case", "mail system"},
			},
		},
	}
}
