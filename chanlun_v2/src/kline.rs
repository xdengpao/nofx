use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Kline {
    pub open_time: i64,
    pub close_time: i64,
    pub open: f64,
    pub high: f64,
    pub low: f64,
    pub close: f64,
    pub volume: f64,
}

/// 经包含关系处理后的K线
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MergedKline {
    pub index: usize,
    pub open_time: i64,
    pub close_time: i64,
    pub high: f64,
    pub low: f64,
    pub direction: Direction,
    /// 合并了多少根原始K线
    pub merged_count: usize,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum Direction {
    #[serde(rename = "up")]
    Up,
    #[serde(rename = "down")]
    Down,
    #[serde(rename = "neutral")]
    Neutral,
}
