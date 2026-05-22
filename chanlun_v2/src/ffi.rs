use std::ffi::{CStr, CString};
use std::os::raw::c_char;
use serde::{Deserialize, Serialize};
use crate::analyzer::{self, AnalysisConfig, AnalysisResult};
use crate::kline::Kline;

#[derive(Deserialize)]
struct AnalyzeInput {
    klines: Vec<Kline>,
    macd_hist: Vec<f64>,
    #[serde(default)]
    config: AnalysisConfig,
}

#[derive(Serialize)]
struct AnalyzeOutput {
    success: bool,
    error: String,
    result: Option<AnalysisResult>,
}

/// 全量分析：输入 JSON，输出 JSON
/// 返回写入 output_buf 的字节数，负数表示错误
#[no_mangle]
pub extern "C" fn chanlun_analyze(
    input_ptr: *const c_char,
    output_buf: *mut c_char,
    output_len: i32,
) -> i32 {
    let input_str = unsafe {
        if input_ptr.is_null() { return -1; }
        match CStr::from_ptr(input_ptr).to_str() {
            Ok(s) => s,
            Err(_) => return -2,
        }
    };

    let input: AnalyzeInput = match serde_json::from_str(input_str) {
        Ok(v) => v,
        Err(_) => {
            return write_error(output_buf, output_len, "invalid input JSON");
        }
    };

    let result = analyzer::analyze(&input.klines, &input.macd_hist, &input.config);
    let output = AnalyzeOutput { success: true, error: String::new(), result: Some(result) };
    write_output(output_buf, output_len, &output)
}

/// 释放由 Rust 分配的 CString
#[no_mangle]
pub extern "C" fn chanlun_free_string(ptr: *mut c_char) {
    if !ptr.is_null() {
        unsafe { drop(CString::from_raw(ptr)); }
    }
}

fn write_output(buf: *mut c_char, len: i32, output: &AnalyzeOutput) -> i32 {
    let json = match serde_json::to_string(output) {
        Ok(s) => s,
        Err(_) => return -3,
    };
    let bytes = json.as_bytes();
    if bytes.len() >= len as usize {
        return -4; // buffer too small
    }
    unsafe {
        std::ptr::copy_nonoverlapping(bytes.as_ptr(), buf as *mut u8, bytes.len());
        *buf.add(bytes.len()) = 0; // null terminate
    }
    bytes.len() as i32
}

fn write_error(buf: *mut c_char, len: i32, msg: &str) -> i32 {
    let output = AnalyzeOutput { success: false, error: msg.to_string(), result: None };
    write_output(buf, len, &output)
}
