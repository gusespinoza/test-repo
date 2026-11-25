import { HttpClient } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { CreateBookingRequest, Order } from './bookings.models';

@Injectable({ providedIn: 'root' })
export class BookingsService {
  private readonly http = inject(HttpClient);
  private readonly endpoint = '/api/bookings';

  createBooking(input: CreateBookingRequest): Observable<Order> {
    return this.http.post<Order>(this.endpoint, input);
  }

  getBooking(id: string): Observable<Order> {
    return this.http.get<Order>(`${this.endpoint}/${encodeURIComponent(id)}`);
  }

  confirmBooking(id: string): Observable<Order> {
    return this.http.post<Order>(`${this.endpoint}/${encodeURIComponent(id)}/confirm`, {});
  }

  cancelBooking(id: string): Observable<Order> {
    return this.http.delete<Order>(`${this.endpoint}/${encodeURIComponent(id)}`);
  }
}
