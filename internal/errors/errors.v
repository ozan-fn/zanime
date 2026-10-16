module errors

// Kind: jenis error penerus (400/404/429/502).
pub enum Kind {
	upstream
	not_found
	bad_request
	rate_limited
}

// AppErr implement IError: msg() + code() = status HTTP.
// `str()` sengaja tak dipakai (IError.str bawaan menambah "; code: N"), pemanggil
// membaca msg()/code() langsung.
pub struct AppErr {
pub:
	kind Kind
	msg  string
}

pub fn (e AppErr) msg() string {
	return match e.kind {
		.upstream { 'upstream error: ${e.msg}' }
		else { e.msg }
	}
}

pub fn (e AppErr) code() int {
	return match e.kind {
		.upstream { 502 }
		.not_found { 404 }
		.bad_request { 400 }
		.rate_limited { 429 }
	}
}

// is_upstream: untuk membedakan pesan mentah dari error IError lain.
pub fn is_app(err IError) bool {
	return err is AppErr
}

pub fn upstream(msg string) IError {
	return AppErr{
		kind: .upstream
		msg:  msg
	}
}

pub fn not_found(msg string) IError {
	return AppErr{
		kind: .not_found
		msg:  msg
	}
}

pub fn bad_request(msg string) IError {
	return AppErr{
		kind: .bad_request
		msg:  msg
	}
}

pub fn rate_limited(msg string) IError {
	return AppErr{
		kind: .rate_limited
		msg:  msg
	}
}
